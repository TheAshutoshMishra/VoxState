package policy

import (
	"errors"
	"testing"
	"time"

	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
)

// newTestEvaluator builds a state.Store + tasks.Store + Evaluator trio
// with a machine already created at v1.
func newTestEvaluator(t *testing.T) (*state.Store, *tasks.Store, *Evaluator) {
	t.Helper()
	ss := state.NewStore()
	_, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"})
	if err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	ts := tasks.NewStore(ss, nil)
	return ss, ts, NewEvaluator(ss)
}

func changeMachineState(t *testing.T, ss *state.Store, machineID string) {
	t.Helper()
	_, changed, _, err := ss.ChangeState(machineID, state.StateChangeInput{
		Attributes: map[string]string{"probe": "1"},
	})
	if err != nil {
		t.Fatalf("ChangeState() error = %v", err)
	}
	if !changed {
		t.Fatal("expected ChangeState to actually change the state")
	}
}

func TestEvaluate_MatchingVersion_Accepted(t *testing.T) {
	_, _, ev := newTestEvaluator(t)

	result := ToolResult{ID: "result-1", TaskID: "task-1", MachineID: "machine-17", BoundVersion: 1}
	decision, err := ev.Evaluate(result)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Outcome != Accepted {
		t.Errorf("Outcome = %q, want %q", decision.Outcome, Accepted)
	}
	if !decision.IsAccepted() {
		t.Error("IsAccepted() = false, want true")
	}
	if decision.CurrentVersion != 1 {
		t.Errorf("CurrentVersion = %d, want 1", decision.CurrentVersion)
	}
	if decision.Reason != "" {
		t.Errorf("Reason = %q, want empty for an accepted decision", decision.Reason)
	}
	if decision.Event.Type != "" {
		t.Errorf("Event.Type = %q, want zero value for an accepted decision", decision.Event.Type)
	}
}

func TestEvaluate_OlderVersion_Rejected(t *testing.T) {
	ss, _, ev := newTestEvaluator(t)
	changeMachineState(t, ss, "machine-17") // machine -> v2

	result := ToolResult{ID: "result-1", TaskID: "task-1", MachineID: "machine-17", BoundVersion: 1}
	decision, err := ev.Evaluate(result)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Outcome != Rejected {
		t.Errorf("Outcome = %q, want %q", decision.Outcome, Rejected)
	}
	if decision.IsAccepted() {
		t.Error("IsAccepted() = true, want false for a stale result")
	}
	if decision.Reason == "" {
		t.Error("Reason is empty, want an explanation")
	}
}

func TestEvaluate_FutureVersion_Rejected(t *testing.T) {
	ss, _, ev := newTestEvaluator(t)
	changeMachineState(t, ss, "machine-17") // machine -> v2

	// result claims to be bound to a version the machine hasn't reached
	// yet — proves the check is exact equality, not just "< current".
	result := ToolResult{ID: "result-1", TaskID: "task-1", MachineID: "machine-17", BoundVersion: 3}
	decision, err := ev.Evaluate(result)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Outcome != Rejected {
		t.Errorf("Outcome = %q, want %q", decision.Outcome, Rejected)
	}
	if decision.CurrentVersion != 2 {
		t.Errorf("CurrentVersion = %d, want 2", decision.CurrentVersion)
	}
}

func TestEvaluate_RejectionEventContainsVersions(t *testing.T) {
	ss, _, ev := newTestEvaluator(t)
	changeMachineState(t, ss, "machine-17") // machine -> v2

	result := ToolResult{ID: "result-abc", TaskID: "task-xyz", MachineID: "machine-17", BoundVersion: 1}
	decision, err := ev.Evaluate(result)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if decision.Event.Type != "ToolResultRejected" {
		t.Fatalf("Event.Type = %q, want %q", decision.Event.Type, "ToolResultRejected")
	}
	if decision.Event.MachineID != "machine-17" {
		t.Errorf("Event.MachineID = %q, want %q", decision.Event.MachineID, "machine-17")
	}
	if got := decision.Event.Payload["task_id"]; got != "task-xyz" {
		t.Errorf("Event.Payload[task_id] = %v, want %q", got, "task-xyz")
	}
	if got := decision.Event.Payload["result_id"]; got != "result-abc" {
		t.Errorf("Event.Payload[result_id] = %v, want %q", got, "result-abc")
	}
	if got := decision.Event.Payload["result_version"]; got != 1 {
		t.Errorf("Event.Payload[result_version] = %v, want 1", got)
	}
	if got := decision.Event.Payload["current_version"]; got != 2 {
		t.Errorf("Event.Payload[current_version] = %v, want 2", got)
	}
	if decision.Event.Payload["reason"] == "" {
		t.Error("Event.Payload[reason] is empty")
	}
}

func TestSimulateToolResult_CopiesTaskBoundVersion(t *testing.T) {
	ss, ts, _ := newTestEvaluator(t)

	task, _, err := ts.CreateTask(tasks.CreateTaskInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.BoundVersion != 1 {
		t.Fatalf("task.BoundVersion = %d, want 1", task.BoundVersion)
	}

	changeMachineState(t, ss, "machine-17") // machine -> v2, task stays bound to v1

	result := SimulateToolResult(task, map[string]any{"reading": "ok"})
	if result.BoundVersion != 1 {
		t.Errorf("result.BoundVersion = %d, want task's bound version 1 (not the machine's current version)", result.BoundVersion)
	}
	if result.TaskID != task.ID {
		t.Errorf("result.TaskID = %q, want %q", result.TaskID, task.ID)
	}
	if result.MachineID != task.MachineID {
		t.Errorf("result.MachineID = %q, want %q", result.MachineID, task.MachineID)
	}
}

func TestEvaluate_DoesNotMutateMachineState(t *testing.T) {
	ss, _, ev := newTestEvaluator(t)
	changeMachineState(t, ss, "machine-17") // machine -> v2

	before, err := ss.GetCurrentState("machine-17")
	if err != nil {
		t.Fatalf("GetCurrentState() error = %v", err)
	}

	result := ToolResult{ID: "result-1", TaskID: "task-1", MachineID: "machine-17", BoundVersion: 1}
	if _, err := ev.Evaluate(result); err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	after, err := ss.GetCurrentState("machine-17")
	if err != nil {
		t.Fatalf("GetCurrentState() error = %v", err)
	}
	if after.Version != before.Version {
		t.Errorf("machine version changed from %d to %d as a side effect of Evaluate", before.Version, after.Version)
	}
	if after.Status != before.Status {
		t.Errorf("machine status changed from %q to %q as a side effect of Evaluate", before.Status, after.Status)
	}
}

func TestEvaluate_DoesNotMutateTaskBoundVersion(t *testing.T) {
	ss, ts, ev := newTestEvaluator(t)

	task, _, err := ts.CreateTask(tasks.CreateTaskInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	changeMachineState(t, ss, "machine-17") // machine -> v2

	result := SimulateToolResult(task, nil)
	if _, err := ev.Evaluate(result); err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	after, err := ts.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if after.BoundVersion != 1 {
		t.Errorf("task.BoundVersion after Evaluate = %d, want unchanged 1", after.BoundVersion)
	}
}

func TestEvaluate_MissingMachineID(t *testing.T) {
	_, _, ev := newTestEvaluator(t)

	_, err := ev.Evaluate(ToolResult{BoundVersion: 1})
	if !errors.Is(err, ErrMachineIDRequired) {
		t.Errorf("err = %v, want ErrMachineIDRequired", err)
	}
}

func TestEvaluate_InvalidBoundVersion(t *testing.T) {
	_, _, ev := newTestEvaluator(t)

	_, err := ev.Evaluate(ToolResult{MachineID: "machine-17", BoundVersion: 0})
	if !errors.Is(err, ErrInvalidBoundVersion) {
		t.Errorf("err = %v, want ErrInvalidBoundVersion", err)
	}
}

func TestEvaluate_MachineNotFound(t *testing.T) {
	_, _, ev := newTestEvaluator(t)

	_, err := ev.Evaluate(ToolResult{MachineID: "does-not-exist", BoundVersion: 1})
	if !errors.Is(err, state.ErrMachineNotFound) {
		t.Errorf("err = %v, want state.ErrMachineNotFound", err)
	}
}

// TestStaleResultRejectedAfterStateChange is the project's central
// correctness invariant, spelled out end to end exactly as described in
// the M4 brief:
//
//	v1 -> task bound to v1 -> machine -> v2 -> result bound to v1 -> policy -> REJECT
//
// This is the scenario the whole VoxState project exists to demonstrate.
func TestStaleResultRejectedAfterStateChange(t *testing.T) {
	ss, ts, ev := newTestEvaluator(t)

	// 1-2. Machine already created at v1 by newTestEvaluator.
	initial, err := ss.GetCurrentState("machine-17")
	if err != nil {
		t.Fatalf("GetCurrentState() error = %v", err)
	}
	if initial.Version != 1 {
		t.Fatalf("initial machine version = %d, want 1", initial.Version)
	}

	// 3-4. Create diagnostic task; it captures BoundVersion = 1.
	task, _, err := ts.CreateTask(tasks.CreateTaskInput{MachineID: "machine-17", ToolName: "vibration_scan"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.BoundVersion != 1 {
		t.Fatalf("task.BoundVersion = %d, want 1", task.BoundVersion)
	}

	// 5. Start simulated diagnostic work (asynchronous, per M3) — long
	// enough that it's still running when we change machine state below.
	if _, err := ts.StartTask(task.ID, tasks.SimulatedWork(200*time.Millisecond)); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}

	// 6-7. Change machine state while the task is still running and bound
	// to v1; machine becomes v2.
	changeMachineState(t, ss, "machine-17")
	current, err := ss.GetCurrentState("machine-17")
	if err != nil {
		t.Fatalf("GetCurrentState() error = %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("machine version after change = %d, want 2", current.Version)
	}

	// 8. Wait for the diagnostic to actually finish, then build its result.
	// The task's BoundVersion never changed, so the simulated result
	// inherits v1 regardless of what the machine has moved on to.
	got := waitForTaskCompletion(t, ts, task.ID)
	if got.BoundVersion != 1 {
		t.Fatalf("task.BoundVersion after machine moved to v2 = %d, want still 1", got.BoundVersion)
	}
	result := SimulateToolResult(got, map[string]any{"vibration": "CRITICAL"})
	if result.BoundVersion != 1 {
		t.Fatalf("result.BoundVersion = %d, want 1", result.BoundVersion)
	}

	// 9-10. Run policy evaluation; it must reject.
	decision, err := ev.Evaluate(result)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Outcome != Rejected {
		t.Fatalf("Outcome = %q, want %q", decision.Outcome, Rejected)
	}
	if decision.IsAccepted() {
		t.Fatal("IsAccepted() = true, want false: a stale result must never be treated as accepted")
	}

	// 11. ToolResultRejected event is produced.
	if decision.Event.Type != "ToolResultRejected" {
		t.Fatalf("Event.Type = %q, want %q", decision.Event.Type, "ToolResultRejected")
	}
	if decision.Event.Payload["result_version"] != 1 {
		t.Errorf("Event.Payload[result_version] = %v, want 1", decision.Event.Payload["result_version"])
	}
	if decision.Event.Payload["current_version"] != 2 {
		t.Errorf("Event.Payload[current_version] = %v, want 2", decision.Event.Payload["current_version"])
	}

	// 12. Result is NOT accepted (re-asserted via the explicit Decision type).
	if decision.Outcome == Accepted {
		t.Fatal("a stale result was accepted — the core invariant of this project is broken")
	}
}

// TestFreshResultAfterStateUpdate_Accepted verifies the counterpart to
// the stale-result test: a result bound to the machine's current version
// after an update passes the policy check.
func TestFreshResultAfterStateUpdate_Accepted(t *testing.T) {
	ss, ts, ev := newTestEvaluator(t)
	changeMachineState(t, ss, "machine-17") // machine -> v2

	// A task created *after* the change is bound to the new version.
	task, _, err := ts.CreateTask(tasks.CreateTaskInput{MachineID: "machine-17"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.BoundVersion != 2 {
		t.Fatalf("task.BoundVersion = %d, want 2", task.BoundVersion)
	}

	result := SimulateToolResult(task, map[string]any{"vibration": "NORMAL"})
	decision, err := ev.Evaluate(result)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Outcome != Accepted {
		t.Fatalf("Outcome = %q, want %q", decision.Outcome, Accepted)
	}
	if !decision.IsAccepted() {
		t.Fatal("IsAccepted() = false, want true for a fresh result")
	}
}

// waitForTaskCompletion polls until a task reaches COMPLETED or the test
// times out. Polling (not a fixed sleep) is used because completion
// happens on a goroutine whose exact scheduling isn't controlled by the
// test — see internal/tasks's own waitForStatus helper for the same
// pattern.
func waitForTaskCompletion(t *testing.T, ts *tasks.Store, id string) tasks.DiagnosticTask {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := ts.GetTask(id)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if task.Status == tasks.StatusCompleted {
			return task
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach COMPLETED within timeout", id)
	return tasks.DiagnosticTask{}
}
