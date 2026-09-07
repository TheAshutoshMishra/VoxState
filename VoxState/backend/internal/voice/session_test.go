// This file's fakes (fakeSTT/fakeTTS/fakeTransport/fakeTool) and its
// tests never import voice/livekit, voice/rime, or voice/deepgram —
// voice's own tests exercise the leak-proof TextResponse boundary and
// the agent.Run orchestration against real state/tasks/policy/tools
// packages (same newTestAgent-style pattern as agent_test.go), with only
// the voice-specific STT/TTS/Transport boundary faked out. No live
// network call happens anywhere in this package's test suite.
package voice

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/tools"
)

// fakeSTT returns a scripted transcript, ignoring the audio it's given —
// tests drive "what the user said" directly rather than needing a real
// codec/decoder in the loop.
type fakeSTT struct {
	transcript string
}

func (f fakeSTT) Transcribe(context.Context, []int16, int) (string, error) {
	return f.transcript, nil
}

// fakeTTS records every text it's asked to synthesize — the assertion
// surface for the stale-result test below.
type fakeTTS struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeTTS) Synthesize(_ context.Context, text string) ([]int16, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, text)
	return []int16{0, 0}, 16000, nil
}

func (f *fakeTTS) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeTransport records outbound buffers sent via Send. Frames() is
// unused by the tests below — they call handleUtterance directly rather
// than driving Run's frame-consuming loop, so segmenter timing never
// makes these tests flaky (see session.go's doc comment on
// handleUtterance for why it's a directly-testable seam).
type fakeTransport struct {
	mu     sync.Mutex
	sent   [][]int16
	frames chan AudioFrame
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{frames: make(chan AudioFrame)}
}

func (f *fakeTransport) Send(_ context.Context, audio []int16, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, audio)
	return nil
}

func (f *fakeTransport) SentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeTransport) Frames() <-chan AudioFrame { return f.frames }
func (f *fakeTransport) Close() error              { close(f.frames); return nil }

// fakeTool mirrors agent_test.go's fakeTool exactly, replicated here
// rather than shared, per this codebase's convention of package-local
// test fakes (see agent_test.go's own header note).
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

// newTestAgent builds a real state/task/policy/agent stack with a
// machine already created at v1 and the real, zero-delay demo tools
// registered — mirroring agent_test.go's own helper of the same name.
func newTestAgent(t *testing.T) (*state.Store, *tasks.Store, *agent.Agent) {
	t.Helper()
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	ts := tasks.NewStore(ss, nil)
	ev := policy.NewEvaluator(ss)
	registry := tools.NewRegistry(tools.NewVibrationScan(0), tools.NewTemperatureScan(0))
	ag := agent.New(ss, ts, ev, registry, nil)
	return ss, ts, ag
}

func newFakeToolAgent(t *testing.T, started chan<- struct{}, finish <-chan struct{}) (*state.Store, *tasks.Store, *agent.Agent) {
	t.Helper()
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	ts := tasks.NewStore(ss, nil)
	ev := policy.NewEvaluator(ss)
	registry := tools.NewRegistry(fakeTool{name: tools.NameVibrationScan, started: started, finish: finish})
	ag := agent.New(ss, ts, ev, registry, nil)
	return ss, ts, ag
}

func TestSessionBasicRequestEndToEnd(t *testing.T) {
	_, _, ag := newTestAgent(t)
	tts := &fakeTTS{}
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, fakeSTT{transcript: "please check vibration"}, tts, transport, nil)

	result, err := session.handleUtterance(context.Background(), []int16{1, 2, 3}, 16000)
	if err != nil {
		t.Fatalf("handleUtterance() error = %v", err)
	}
	if result.Outcome != agent.OutcomeAccepted {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, agent.OutcomeAccepted)
	}

	calls := tts.Calls()
	if len(calls) != 1 {
		t.Fatalf("len(tts.Calls()) = %d, want 1", len(calls))
	}
	if !strings.Contains(calls[0], "NORMAL") {
		t.Errorf("TTS text = %q, want it to mention the accepted payload", calls[0])
	}

	if got := transport.SentCount(); got != 1 {
		t.Fatalf("transport.SentCount() = %d, want 1", got)
	}
}

func TestSessionStateAwareResponseBoundToVersion(t *testing.T) {
	ss, ts, ag := newTestAgent(t)
	if _, changed, _, err := ss.ChangeState("machine-17", state.StateChangeInput{
		Attributes: map[string]string{"probe": "1"},
	}); err != nil || !changed {
		t.Fatalf("ChangeState() changed=%v err=%v", changed, err)
	}

	tts := &fakeTTS{}
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, fakeSTT{transcript: "check vibration"}, tts, transport, nil)

	result, err := session.handleUtterance(context.Background(), []int16{1}, 16000)
	if err != nil {
		t.Fatalf("handleUtterance() error = %v", err)
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

// TestSessionNeverSpeaksStaleResult is M6's flagship test, mirroring
// agent_test.go's TestAgentRejectsStaleResult exactly, but asserting at
// the TTS boundary rather than the agent.Result boundary: the machine
// changes state WHILE the tool is still running (bound to v1), so the
// tool's eventual result is stale by the time it arrives and must be
// rejected — and, critically, its payload content must never reach the
// text handed to TTS.Synthesize.
func TestSessionNeverSpeaksStaleResult(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	ss, ts, ag := newFakeToolAgent(t, started, finish)

	tts := &fakeTTS{}
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, fakeSTT{transcript: "check vibration"}, tts, transport, nil)

	type outcome struct {
		result agent.Result
		err    error
	}
	doneCh := make(chan outcome, 1)
	go func() {
		result, err := session.handleUtterance(context.Background(), []int16{1}, 16000)
		doneCh <- outcome{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool never started")
	}

	// Machine moves to v2 while the tool is still "in flight", bound to v1.
	if _, changed, _, err := ss.ChangeState("machine-17", state.StateChangeInput{
		Attributes: map[string]string{"vibration": "CRITICAL"},
	}); err != nil || !changed {
		t.Fatalf("ChangeState() changed=%v err=%v", changed, err)
	}

	close(finish) // let the tool "finish" now, producing its (stale) v1 result

	var got outcome
	select {
	case got = <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("handleUtterance() did not return")
	}
	if got.err != nil {
		t.Fatalf("handleUtterance() error = %v", got.err)
	}
	if got.result.Outcome != agent.OutcomeRejected {
		t.Fatalf("Outcome = %q, want %q", got.result.Outcome, agent.OutcomeRejected)
	}

	task, err := ts.GetTask(got.result.TaskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.BoundVersion != 1 {
		t.Errorf("task.BoundVersion after machine moved to v2 = %d, want still 1", task.BoundVersion)
	}

	// The core assertion: every recorded TTS call — if any — must never
	// contain the stale tool's payload marker value. Checking for absence
	// of the marker string is a stronger test than only checking
	// Outcome == Rejected, since it also protects against a hypothetical
	// future bug in TextResponse leaking payload content through some
	// other field.
	for _, call := range tts.Calls() {
		if strings.Contains(call, "CRITICAL") {
			t.Fatalf("TTS was asked to speak stale payload content: %q", call)
		}
	}
}

func TestSessionPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish) // safety net against a goroutine leak

	_, ts, ag := newFakeToolAgent(t, started, finish)
	tts := &fakeTTS{}
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, fakeSTT{transcript: "check vibration"}, tts, transport, nil)

	ctx, cancel := context.WithCancel(context.Background())

	type outcome struct {
		result agent.Result
		err    error
	}
	doneCh := make(chan outcome, 1)
	go func() {
		result, err := session.handleUtterance(ctx, []int16{1}, 16000)
		doneCh <- outcome{result: result, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool never started")
	}

	cancel()

	select {
	case got := <-doneCh:
		if got.err == nil {
			t.Error("handleUtterance() error = nil, want context.Canceled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleUtterance() did not return after context cancellation")
	}

	list, err := ts.ListTasksForMachine("machine-17")
	if err != nil {
		t.Fatalf("ListTasksForMachine() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].Status != tasks.StatusCancelled {
		t.Errorf("task.Status = %q, want %q", list[0].Status, tasks.StatusCancelled)
	}
}
