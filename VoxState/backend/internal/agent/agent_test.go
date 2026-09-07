package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/tools"
)

// newTestAgent builds a state/task/policy/agent stack with a machine
// already created at v1 and the two real (zero-delay, deterministic)
// demo tools registered.
func newTestAgent(t *testing.T) (*state.Store, *tasks.Store, *policy.Evaluator, *Agent) {
	t.Helper()
	ss := state.NewStore()
	_, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"})
	if err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	ts := tasks.NewStore(ss, nil)
	ev := policy.NewEvaluator(ss)
	registry := tools.NewRegistry(tools.NewVibrationScan(0), tools.NewTemperatureScan(0))
	ag := New(ss, ts, ev, registry, nil)
	return ss, ts, ev, ag
}

// fakeTool is test-only infrastructure (mirrors internal/tasks's
// blockingWork pattern from M3): it signals on started once its Run
// begins, then blocks until either finish is closed (simulating the tool
// completing) or its context is cancelled. This is what lets tests
// deliberately interleave a machine state change with "the tool is still
// running".
type fakeTool struct {
	name    string
	started chan<- struct{}
	finish  <-chan struct{}
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) Run(ctx context.Context, _ tools.RunInput) (tools.RunOutput, error) {
	close(f.started)
	select {
	case <-f.finish:
		return tools.RunOutput{Payload: map[string]any{"vibration": "CRITICAL"}}, nil
	case <-ctx.Done():
		return tools.RunOutput{}, ctx.Err()
	}
}

func newFakeToolAgent(t *testing.T, name string, started chan<- struct{}, finish <-chan struct{}) (*state.Store, *tasks.Store, *Agent) {
	t.Helper()
	ss := state.NewStore()
	_, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"})
	if err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	ts := tasks.NewStore(ss, nil)
	ev := policy.NewEvaluator(ss)
	registry := tools.NewRegistry(fakeTool{name: name, started: started, finish: finish})
	ag := New(ss, ts, ev, registry, nil)
	return ss, ts, ag
}

func TestAgentSelectsVibrationTool(t *testing.T) {
	_, _, _, ag := newTestAgent(t)

	result, err := ag.Run(context.Background(), Instruction{MachineID: "machine-17", Text: "please check vibration"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.SelectedTool != tools.NameVibrationScan {
		t.Errorf("SelectedTool = %q, want %q", result.SelectedTool, tools.NameVibrationScan)
	}
}

func TestAgentCreatesTaskWithCurrentStateVersion(t *testing.T) {
	ss, ts, _, ag := newTestAgent(t)

	// Bump the machine forward so "bound to current version" isn't
	// trivially just "bound to 1".
	_, changed, _, err := ss.ChangeState("machine-17", state.StateChangeInput{
		Attributes: map[string]string{"probe": "1"},
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}
	if !changed {
		t.Fatal("expected ChangeState to actually change the state")
	}

	result, err := ag.Run(context.Background(), Instruction{MachineID: "machine-17", Text: "check vibration"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.BoundVersion != 2 {
		t.Errorf("Result.BoundVersion = %d, want 2", result.BoundVersion)
	}

	task, err := ts.GetTask(result.TaskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.BoundVersion != 2 {
		t.Errorf("task.BoundVersion = %d, want 2", task.BoundVersion)
	}
}

func TestAgentConsumesAcceptedResult(t *testing.T) {
	_, _, _, ag := newTestAgent(t)

	result, err := ag.Run(context.Background(), Instruction{MachineID: "machine-17", Text: "check vibration"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Outcome != OutcomeAccepted {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, OutcomeAccepted)
	}
	if result.Payload["vibration"] != "NORMAL" {
		t.Errorf("Payload[vibration] = %v, want %q", result.Payload["vibration"], "NORMAL")
	}
	if result.RejectionReason != "" {
		t.Errorf("RejectionReason = %q, want empty for an accepted result", result.RejectionReason)
	}
	if result.ReplanRequired {
		t.Error("ReplanRequired = true, want false for an accepted result")
	}
}

// TestAgentRejectsStaleResult is the M5 flagship test (section 12 of the
// milestone brief): the machine changes state WHILE the tool is still
// running, so the tool's result — correctly bound to the version that
// was current when the task was created — is stale by the time it
// arrives. It must be rejected, and the Agent must not treat its payload
// as current truth.
func TestAgentRejectsStaleResult(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	ss, ts, ag := newFakeToolAgent(t, tools.NameVibrationScan, started, finish)

	type runOutcome struct {
		result Result
		err    error
	}
	doneCh := make(chan runOutcome, 1)
	go func() {
		result, err := ag.Run(context.Background(), Instruction{MachineID: "machine-17", Text: "check vibration"})
		doneCh <- runOutcome{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool never started")
	}

	// Machine moves to v2 while the tool is still "in flight", bound to v1.
	_, changed, _, err := ss.ChangeState("machine-17", state.StateChangeInput{
		Attributes: map[string]string{"vibration": "CRITICAL"},
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}
	if !changed {
		t.Fatal("expected ChangeState to actually change the state")
	}

	close(finish) // let the tool "finish" now, producing its (stale) v1 result

	var got runOutcome
	select {
	case got = <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return")
	}
	if got.err != nil {
		t.Fatalf("Run() error = %v", got.err)
	}
	result := got.result

	// The explicit section-12 checklist:
	if result.BoundVersion != 1 {
		t.Errorf("BoundVersion = %d, want 1", result.BoundVersion)
	}
	if result.CurrentVersion != 2 {
		t.Errorf("CurrentVersion = %d, want 2", result.CurrentVersion)
	}
	if result.Outcome != OutcomeRejected {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, OutcomeRejected)
	}
	if result.Payload != nil {
		t.Errorf("Payload = %v, want nil: the Agent must never consume a stale result", result.Payload)
	}
	if result.RejectionReason == "" {
		t.Error("RejectionReason is empty, want an explanation")
	}
	if !result.ReplanRequired {
		t.Error("ReplanRequired = false, want true")
	}

	task, err := ts.GetTask(result.TaskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.BoundVersion != 1 {
		t.Errorf("task.BoundVersion after machine moved to v2 = %d, want still 1", task.BoundVersion)
	}
}

// TestAgentDoesNotConsumeStalePayload proves the boundary is specific to
// stale results, not just "Payload happens to always be nil": the same
// underlying stores see one Run() that races to REJECTED (payload absent)
// and a second, uncontested Run() that reaches ACCEPTED (payload
// present) — showing the Agent genuinely gates on the policy Decision
// rather than never surfacing a payload at all.
func TestAgentDoesNotConsumeStalePayload(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	ss, ts, staleAgent := newFakeToolAgent(t, tools.NameVibrationScan, started, finish)

	type runOutcome struct {
		result Result
		err    error
	}
	doneCh := make(chan runOutcome, 1)
	go func() {
		result, err := staleAgent.Run(context.Background(), Instruction{MachineID: "machine-17", Text: "check vibration"})
		doneCh <- runOutcome{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool never started")
	}

	if _, changed, _, err := ss.ChangeState("machine-17", state.StateChangeInput{
		Attributes: map[string]string{"vibration": "CRITICAL"},
	}); err != nil || !changed {
		t.Fatalf("ChangeState() changed=%v err=%v", changed, err)
	}
	close(finish)

	var got runOutcome
	select {
	case got = <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return")
	}
	if got.err != nil {
		t.Fatalf("Run() error = %v", got.err)
	}
	if got.result.Outcome != OutcomeRejected {
		t.Fatalf("Outcome = %q, want %q", got.result.Outcome, OutcomeRejected)
	}
	if got.result.Payload != nil {
		t.Fatalf("Payload = %v, want nil for a rejected result", got.result.Payload)
	}

	// Contrast: a fresh, uncontested request against the now-current
	// version (v2) must still be able to reach ACCEPTED with a payload —
	// proving the Agent isn't just "always empty", it's specifically
	// withholding the stale one.
	freshEvaluator := policy.NewEvaluator(ss)
	freshRegistry := tools.NewRegistry(tools.NewVibrationScan(0))
	freshAgent := New(ss, ts, freshEvaluator, freshRegistry, nil)

	freshResult, err := freshAgent.Run(context.Background(), Instruction{MachineID: "machine-17", Text: "check vibration"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if freshResult.Outcome != OutcomeAccepted {
		t.Fatalf("fresh Outcome = %q, want %q", freshResult.Outcome, OutcomeAccepted)
	}
	if freshResult.Payload == nil {
		t.Error("fresh Payload is nil, want a consumed payload for an accepted result")
	}
}

// TestAgentRejectsFutureVersionResult guards against a regression where
// the version check becomes "<" instead of "==". Run's own flow can never
// naturally produce a result whose BoundVersion is ahead of the machine's
// current version — a task is always bound to whatever version is
// current at creation time, and versions only move forward afterward —
// so this test exercises the same Evaluator instance Run delegates to
// directly, with a fabricated future-version result.
func TestAgentRejectsFutureVersionResult(t *testing.T) {
	_, _, ev, _ := newTestAgent(t)

	result := policy.ToolResult{
		ID:           "result-future",
		TaskID:       "task-future",
		MachineID:    "machine-17",
		BoundVersion: 99,
	}
	decision, err := ev.Evaluate(result)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Outcome != policy.Rejected {
		t.Fatalf("Outcome = %q, want %q", decision.Outcome, policy.Rejected)
	}
	if decision.IsAccepted() {
		t.Error("IsAccepted() = true, want false for a version ahead of current")
	}
}

func TestAgentPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish) // safety net: avoid a goroutine leak if cancellation didn't reach the tool as expected

	_, ts, ag := newFakeToolAgent(t, tools.NameVibrationScan, started, finish)

	ctx, cancel := context.WithCancel(context.Background())

	type runOutcome struct {
		result Result
		err    error
	}
	doneCh := make(chan runOutcome, 1)
	go func() {
		result, err := ag.Run(ctx, Instruction{MachineID: "machine-17", Text: "check vibration"})
		doneCh <- runOutcome{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool never started")
	}

	start := time.Now()
	cancel()

	var got runOutcome
	select {
	case got = <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Run() took %v to return after cancellation, want a prompt return", elapsed)
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Errorf("Run() error = %v, want context.Canceled", got.err)
	}

	list, err := ts.ListTasksForMachine("machine-17")
	if err != nil {
		t.Fatalf("ListTasksForMachine() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].Status != tasks.StatusCancelled {
		t.Errorf("task.Status = %q, want %q — Run must propagate its own context cancellation into the task engine via CancelTask", list[0].Status, tasks.StatusCancelled)
	}
}
