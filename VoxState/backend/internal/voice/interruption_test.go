// M7's test suite. Every test here drives either VoiceSession.Run's real
// frame loop (via fakeTransport.frames) or VoiceSession.handleUtterance
// directly, exactly like session_test.go's M6 tests — no live network
// call, no dependence on real wall-clock timing: segmenter timeouts are
// driven by AudioFrame sample counts (frameDuration is pure arithmetic,
// see session.go), not by how long a test actually sleeps.
package voice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// --- shared fakes/helpers for this file only -------------------------------

// scriptedSTT returns successive transcripts from a fixed script,
// ignoring audio content — this file's tests need two distinct turns
// ("check vibration" then "check temperature") going through the same
// session, which fakeSTT's single fixed transcript (session_test.go)
// can't express.
type scriptedSTT struct {
	mu          sync.Mutex
	transcripts []string
	calls       int
}

func (s *scriptedSTT) Transcribe(context.Context, []int16, int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.transcripts) {
		return "", nil
	}
	text := s.transcripts[s.calls]
	s.calls++
	return text, nil
}

// repeatableFakeTool is session_test.go's fakeTool generalized to survive
// more than one invocation: that fakeTool signals "started" by closing a
// channel, which panics the second time the same Tool value is invoked
// (registries hold one Tool instance for the lifetime of the Agent, and
// this file's rapid-double-interruption test deliberately triggers the
// same tool name twice). Sending on started instead of closing it makes
// repeated invocations safe.
type repeatableFakeTool struct {
	name    string
	started chan<- struct{}
	finish  <-chan struct{}
}

func (f repeatableFakeTool) Name() string { return f.name }

func (f repeatableFakeTool) Run(ctx context.Context, _ tools.RunInput) (tools.RunOutput, error) {
	select {
	case f.started <- struct{}{}:
	case <-ctx.Done():
		return tools.RunOutput{}, ctx.Err()
	}
	select {
	case <-f.finish:
		return tools.RunOutput{Payload: map[string]any{"vibration": "CRITICAL"}}, nil
	case <-ctx.Done():
		return tools.RunOutput{}, ctx.Err()
	}
}

// newInterruptTestAgent registers both a controllable fake vibration_scan
// (blocks on started/finish) and the real, zero-delay temperature_scan
// tool, so a "check vibration" turn can be held open under test control
// while a later "check temperature" turn in the same test still runs and
// completes normally.
func newInterruptTestAgent(t *testing.T, started chan<- struct{}, finish <-chan struct{}) (*state.Store, *tasks.Store, *agent.Agent) {
	t.Helper()
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		t.Fatalf("CreateMachine() error = %v", err)
	}
	ts := tasks.NewStore(ss, nil)
	ev := policy.NewEvaluator(ss)
	registry := tools.NewRegistry(
		repeatableFakeTool{name: tools.NameVibrationScan, started: started, finish: finish},
		tools.NewTemperatureScan(0),
	)
	ag := agent.New(ss, ts, ev, registry, nil)
	return ss, ts, ag
}

// capturingHandler is a minimal slog.Handler that records every log
// record's message and attributes, so tests can assert on the M7
// logging/event requirements (turn_started/turn_interrupted/
// response_cancelled/audio_stopped/turn_completed, and the
// UserInterrupted/ResponseInvalidated events voice logs) without parsing
// stdout.
type capturingHandler struct {
	mu      sync.Mutex
	records []capturedRecord
}

type capturedRecord struct {
	msg   string
	attrs map[string]any
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, capturedRecord{msg: r.Message, attrs: attrs})
	h.mu.Unlock()
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

func (h *capturingHandler) find(msg string) []capturedRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []capturedRecord
	for _, r := range h.records {
		if r.msg == msg {
			out = append(out, r)
		}
	}
	return out
}

func newCapturingLogger() (*slog.Logger, *capturingHandler) {
	h := &capturingHandler{}
	return slog.New(h), h
}

// waitFor polls cond until it's true or timeout elapses. The work being
// waited on in this file's tests is always either a channel send/receive
// already established to have happened (started, etc.) or in-memory,
// zero-delay goroutine completion — timeout here is a CI-slowness safety
// net, not a substitute for real synchronization.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("condition not met before timeout")
	}
}

// --- Requirement 9: full-duplex acceptance scenario -------------------------

// TestFullDuplex_InterruptionThenNewInstructionBecomesAuthoritative models
// the exact M7 acceptance scenario: request A starts, its tool is held
// open by a controlled/fixed delay, the agent begins producing response
// audio is in flight, the user interrupts, A is cancelled/stopped, and
// only B's response ever reaches TTS/audio output.
func TestFullDuplex_InterruptionThenNewInstructionBecomesAuthoritative(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish) // safety net against a goroutine leak if the test fails before interrupting

	ss, ts, ag := newInterruptTestAgent(t, started, finish)
	stt := &scriptedSTT{transcripts: []string{"check vibration", "check temperature"}}
	tts := &fakeTTS{}
	transport := newFakeTransport()
	logger, logs := newCapturingLogger()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, stt, tts, transport, logger)

	runCtx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- session.Run(runCtx) }()

	// 1-4: request A starts, tool is in flight, agent is "processing".
	transport.frames <- loudFrame(16000, 16000)  // 1s of speech -> utterance A begins
	transport.frames <- quietFrame(16000, 16000) // 1s of silence -> exceeds SilenceTimeout, utterance A is emitted

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("tool A never started")
	}

	// 5-6: user interrupts while A is still running (no response audio
	// has been produced yet, since the tool hasn't returned) — this frame
	// is both the interruption signal and the first frame of B's
	// utterance.
	transport.frames <- loudFrame(16000, 16000)
	// 7: silence closes out B's utterance ("check temperature").
	transport.frames <- quietFrame(16000, 16000)

	// 8-9: B is processed (temperature_scan is real and zero-delay, so
	// this settles quickly).
	waitFor(t, 2*time.Second, func() bool { return len(tts.Calls()) >= 1 })

	cancel()
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("session.Run did not return after ctx cancellation")
	}

	// 10: only B's authoritative response reached TTS/audio output.
	calls := tts.Calls()
	if len(calls) != 1 {
		t.Fatalf("len(tts.Calls()) = %d, want 1 (only turn B should ever speak): %v", len(calls), calls)
	}
	if !strings.Contains(calls[0], "72") {
		t.Errorf("tts call = %q, want it to mention turn B's temperature reading", calls[0])
	}
	if strings.Contains(calls[0], "CRITICAL") {
		t.Fatalf("tts call contains turn A's stale marker: %q", calls[0])
	}
	if got := transport.SentCount(); got != 1 {
		t.Fatalf("transport.SentCount() = %d, want 1", got)
	}
	if got := transport.StopCalls(); got < 1 {
		t.Errorf("transport.StopCalls() = %d, want >= 1 (interruption must stop queued/in-flight audio)", got)
	}

	// Background work for A was cancelled through the existing task
	// engine, not a second cancellation mechanism.
	list, err := ts.ListTasksForMachine("machine-17")
	if err != nil {
		t.Fatalf("ListTasksForMachine() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2 (task A + task B)", len(list))
	}
	taskA, taskB := list[0], list[1]
	if taskA.Status != tasks.StatusCancelled {
		t.Errorf("task A status = %q, want %q", taskA.Status, tasks.StatusCancelled)
	}
	if taskB.Status != tasks.StatusCompleted {
		t.Errorf("task B status = %q, want %q", taskB.Status, tasks.StatusCompleted)
	}

	// Cancelling an already-cancelled task is a defined, non-panicking
	// error, not a silent no-op or a crash (M3's existing guarantee,
	// exercised again here from the voice layer's own call path).
	if _, _, err := ts.CancelTask(taskA.ID); !errors.Is(err, tasks.ErrTaskAlreadyCancelled) {
		t.Errorf("CancelTask(already-cancelled task A) error = %v, want ErrTaskAlreadyCancelled", err)
	}

	_ = ss // referenced only to keep newInterruptTestAgent's return shape obvious at call sites

	// Logging (requirement 14): turn_started/turn_interrupted/
	// response_cancelled/turn_completed all fired, correlated by turn_id,
	// and A's and B's turn_id values are distinct (requirement 1: every
	// active request belongs to one logical turn).
	started1 := logs.find("turn_started")
	if len(started1) != 2 {
		t.Fatalf("turn_started log count = %d, want 2", len(started1))
	}
	turnAID, turnBID := started1[0].attrs["turn_id"], started1[1].attrs["turn_id"]
	if turnAID == "" || turnBID == "" || turnAID == turnBID {
		t.Fatalf("turn ids not distinct/populated: A=%v B=%v", turnAID, turnBID)
	}

	interrupted := logs.find("turn_interrupted")
	if len(interrupted) != 1 {
		t.Fatalf("turn_interrupted log count = %d, want 1", len(interrupted))
	}
	if interrupted[0].attrs["turn_id"] != turnAID {
		t.Errorf("turn_interrupted turn_id = %v, want %v (turn A)", interrupted[0].attrs["turn_id"], turnAID)
	}

	if len(logs.find("audio_stopped")) < 1 {
		t.Error("expected at least one audio_stopped log entry")
	}
	if len(logs.find("response_cancelled")) != 1 {
		t.Errorf("response_cancelled log count = %d, want 1", len(logs.find("response_cancelled")))
	}

	completed := logs.find("turn_completed")
	if len(completed) != 1 || completed[0].attrs["turn_id"] != turnBID {
		t.Fatalf("expected exactly one turn_completed for turn B, got %+v", completed)
	}

	// Events (requirement 15): UserInterrupted + ResponseInvalidated,
	// both scoped to turn A, both carrying identifiers needed to
	// correlate (session_id, turn_id, machine_id).
	events := logs.find("voice session event")
	var sawInterrupted, sawInvalidated bool
	for _, e := range events {
		if e.attrs["machine_id"] != "machine-17" {
			t.Errorf("event machine_id = %v, want machine-17", e.attrs["machine_id"])
		}
		switch fmt.Sprint(e.attrs["event_type"]) {
		case "UserInterrupted":
			sawInterrupted = true
		case "ResponseInvalidated":
			sawInvalidated = true
		}
	}
	if !sawInterrupted || !sawInvalidated {
		t.Errorf("expected both UserInterrupted and ResponseInvalidated events, got %+v", events)
	}
}

// --- Requirement 13: error handling / edge cases ----------------------------

func TestInterrupt_IdleSessionIsANoOp(t *testing.T) {
	_, _, ag := newTestAgent(t)
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, fakeSTT{transcript: ""}, &fakeTTS{}, transport, nil)

	// No panic, no error return path to check — interruptCurrent is a
	// void method precisely because "nothing to interrupt" is a defined,
	// silent no-op, not a failure.
	session.interruptCurrent("idle")

	if got := transport.StopCalls(); got != 0 {
		t.Errorf("transport.StopCalls() = %d, want 0 (nothing was active to stop)", got)
	}
}

func TestInterrupt_SecondInterruptionOnSameTurnIsSafe(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)
	_, _, ag := newInterruptTestAgent(t, started, finish)
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, &scriptedSTT{transcripts: []string{"check vibration"}}, &fakeTTS{}, transport, nil)

	turnCtx, cancel := context.WithCancel(context.Background())
	tn := &turn{id: "turn-test", cancel: cancel}
	session.mu.Lock()
	session.current = tn
	session.mu.Unlock()

	go func() { _, _ = session.handleUtterance(turnCtx, []int16{1}, 16000) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool never started")
	}

	// Interrupting the same *turn twice must not panic (context.Cancel is
	// idempotent) and must leave the session's active-turn bookkeeping
	// consistent.
	session.interrupt(tn, "first")
	session.interrupt(tn, "second")

	if got := transport.StopCalls(); got != 2 {
		t.Errorf("transport.StopCalls() = %d, want 2", got)
	}
	if s := session.activeTurn(); s != nil {
		t.Errorf("activeTurn() = %v, want nil after interruption", s.id)
	}
}

// TestInterrupt_RapidDoubleInterruption exercises interrupting two turns
// in a row (A interrupted by B, B itself interrupted by C before it ever
// finishes) end to end through Run's real frame loop — "second
// interruption" and "new request immediately after interruption" at once.
func TestInterrupt_RapidDoubleInterruption(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)

	ss, ts, ag := newInterruptTestAgent(t, started, finish)
	stt := &scriptedSTT{transcripts: []string{"check vibration", "check vibration", "check temperature"}}
	tts := &fakeTTS{}
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, stt, tts, transport, nil)

	runCtx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- session.Run(runCtx) }()

	// Turn A: "check vibration", held open by the fake tool.
	transport.frames <- loudFrame(16000, 16000)
	transport.frames <- quietFrame(16000, 16000)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("tool A never started")
	}

	// Interrupt A with turn B ("check vibration" again — also blocks,
	// since the fake tool is reused for every vibration_scan call).
	transport.frames <- loudFrame(16000, 16000)
	transport.frames <- quietFrame(16000, 16000)
	select {
	case <-started: // tool B starting
	case <-time.After(2 * time.Second):
		t.Fatal("tool B never started")
	}

	// Interrupt B with turn C ("check temperature" — real, fast tool).
	transport.frames <- loudFrame(16000, 16000)
	transport.frames <- quietFrame(16000, 16000)

	waitFor(t, 2*time.Second, func() bool { return len(tts.Calls()) >= 1 })

	// Interrupting B only cancels its context; the goroutine running its
	// tool still has to observe ctx.Done() and the task engine's
	// completion handler still has to mark it CANCELLED — both happen
	// asynchronously relative to turn C's TTS call, so wait for that to
	// actually land (found flaky under `go test -race -count=20` without
	// this: task B was still RUNNING at the moment of the assertion,
	// even though it always converged to CANCELLED shortly after).
	waitFor(t, 2*time.Second, func() bool {
		list, err := ts.ListTasksForMachine("machine-17")
		return err == nil && len(list) >= 2 && list[1].Status != tasks.StatusRunning
	})

	cancel()
	select {
	case <-runErr:
	case <-time.After(2 * time.Second):
		t.Fatal("session.Run did not return")
	}

	calls := tts.Calls()
	if len(calls) != 1 {
		t.Fatalf("len(tts.Calls()) = %d, want 1 (only turn C should ever speak): %v", len(calls), calls)
	}
	if !strings.Contains(calls[0], "72") {
		t.Errorf("tts call = %q, want it to mention turn C's temperature reading", calls[0])
	}

	list, err := ts.ListTasksForMachine("machine-17")
	if err != nil {
		t.Fatalf("ListTasksForMachine() error = %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len(list) = %d, want 3", len(list))
	}
	if list[0].Status != tasks.StatusCancelled {
		t.Errorf("task A status = %q, want CANCELLED", list[0].Status)
	}
	if list[1].Status != tasks.StatusCancelled {
		t.Errorf("task B status = %q, want CANCELLED", list[1].Status)
	}
	if list[2].Status != tasks.StatusCompleted {
		t.Errorf("task C status = %q, want COMPLETED", list[2].Status)
	}

	_ = ss
}

// --- Requirement 10: cancellation races -------------------------------------

// TestRace_InterruptVsToolCompletion (Race A) repeatedly starts a turn
// whose tool completes with no artificial delay and interrupts it from a
// concurrent goroutine with NO explicit ordering between the two — unlike
// this file's other race tests (which synchronize on started/entered
// channels to land the interrupt at a known point in a turn's lifecycle),
// this is a genuine, unresolvable-in-advance tie: both "the tool finished"
// and "the user interrupted" can become true in the same instant, and
// agent.Run's own runTool select (M5, unchanged by M7) can legitimately
// pick either its resultCh or its ctx.Done() case when both are ready —
// the exact same "whichever acquires the lock/wins the select first wins"
// principle tasks.Store's own CancelTask-vs-finishTask race already
// establishes (see its doc comment and TestCompletionVsCancellationRace).
//
// VoxState does not claim an interrupt always wins a true simultaneous
// tie — doing that would require holding a session-wide lock across TTS
// synthesis (a real network call for the Rime client), which would stop
// Run's frame loop from ever detecting a barge-in while synthesis is in
// flight, a strictly worse trade-off. What this test asserts is the
// achievable, always-true guarantee: no data race (run with -race), no
// panic, and the task engine reaches exactly one consistent terminal
// state every time — never stuck RUNNING, never double-transitioned. The
// practically-relevant guarantee this milestone is actually about — once
// an interruption has landed before a turn speaks, it never speaks — is
// what TestFullDuplex and TestRace_InterruptVsTTSGeneration/
// AudioDelivery above test, with deterministic ordering instead of a
// coin-flip tie.
func TestRace_InterruptVsToolCompletion(t *testing.T) {
	for i := 0; i < 50; i++ {
		ss := state.NewStore()
		if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
			t.Fatalf("CreateMachine() error = %v", err)
		}
		ts := tasks.NewStore(ss, nil)
		ev := policy.NewEvaluator(ss)
		registry := tools.NewRegistry(tools.NewVibrationScan(0)) // real, zero-delay tool: completes essentially immediately
		ag := agent.New(ss, ts, ev, registry, nil)

		tts := &fakeTTS{}
		transport := newFakeTransport()
		session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, &scriptedSTT{transcripts: []string{"check vibration"}}, tts, transport, nil)

		turnCtx, cancel := context.WithCancel(context.Background())
		tn := &turn{id: "turn-race", cancel: cancel}
		session.mu.Lock()
		session.current = tn
		session.mu.Unlock()

		resultCh := make(chan struct {
			result agent.Result
			err    error
		}, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			result, err := session.handleUtterance(turnCtx, []int16{1}, 16000)
			resultCh <- struct {
				result agent.Result
				err    error
			}{result, err}
		}()
		go func() {
			defer wg.Done()
			session.interrupt(tn, "race")
		}()
		wg.Wait()
		<-resultCh

		// This is a true, unresolvable-in-advance tie (see doc comment
		// above) — handleUtterance's Send can legitimately have already
		// happened by the time an error comes back (Race B/C's post-Send
		// StopAudio safety net), so unlike TestFullDuplex and the two race
		// tests below (which land the interrupt at a known point via
		// synchronization), this test does not assert on TTS/Transport
		// call content. What must always hold regardless of which side of
		// the tie won is checked below: no data race (-race), no panic,
		// and the task engine reaching exactly one consistent state.
		list, err := ts.ListTasksForMachine("machine-17")
		if err != nil {
			t.Fatalf("iteration %d: ListTasksForMachine() error = %v", i, err)
		}
		if len(list) != 1 {
			t.Fatalf("iteration %d: len(list) = %d, want 1", i, len(list))
		}

		// The task engine (M3, unchanged) must reach exactly one
		// terminal state regardless of which side of the tie won — no
		// double terminal transition, no task left stuck RUNNING.
		if status := list[0].Status; !status.IsTerminal() {
			t.Fatalf("iteration %d: task status = %q, want a terminal status", i, status)
		}
	}
}

// TestRace_InterruptVsTTSGeneration (Race C, "interruption while TTS is
// generating") uses a TTS whose Synthesize call blocks until released (or
// ctx is cancelled), so the interrupt is guaranteed to land while
// synthesis is genuinely in flight, not before or after it.
func TestRace_InterruptVsTTSGeneration(t *testing.T) {
	_, _, ag := newTestAgent(t)
	entered := make(chan struct{})
	tts := &blockingTTS{entered: entered, release: make(chan struct{})}
	transport := newFakeTransport()
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, &scriptedSTT{transcripts: []string{"check vibration"}}, tts, transport, nil)

	turnCtx, cancel := context.WithCancel(context.Background())
	tn := &turn{id: "turn-tts", cancel: cancel}
	session.mu.Lock()
	session.current = tn
	session.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		_, err := session.handleUtterance(turnCtx, []int16{1}, 16000)
		errCh <- err
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("TTS.Synthesize was never entered")
	}

	session.interrupt(tn, "barge_in")

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handleUtterance() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleUtterance() did not return after interruption")
	}

	if got := transport.SentCount(); got != 0 {
		t.Errorf("transport.SentCount() = %d, want 0 (interrupted response must never be sent)", got)
	}
}

// TestRace_InterruptVsAudioDelivery (Race B / the tail of Race C,
// "interruption while audio is queued") simulates interruption landing
// exactly as Transport.Send returns — the narrowest possible window,
// since Send only enqueues audio for asynchronous playback rather than
// blocking until it's actually heard (see voice/livekit's Transport.Send
// doc comment). handleUtterance's post-Send ctx.Err() check must catch
// this and call StopAudio rather than trust the send already "worked".
func TestRace_InterruptVsAudioDelivery(t *testing.T) {
	_, _, ag := newTestAgent(t)
	transport := newFakeTransport()
	ctx, cancel := context.WithCancel(context.Background())
	sendCancelling := &cancelOnSendTransport{fakeTransport: transport, cancel: cancel}
	session := NewSession("session-1", "machine-17", "room-1", "user-1", ag, &scriptedSTT{transcripts: []string{"check vibration"}}, &fakeTTS{}, sendCancelling, nil)

	result, err := session.handleUtterance(ctx, []int16{1}, 16000)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("handleUtterance() error = %v, want context.Canceled", err)
	}
	if result.Outcome != agent.OutcomeAccepted {
		t.Fatalf("result.Outcome = %q, want ACCEPTED (the underlying work still succeeded; only delivery was interrupted)", result.Outcome)
	}
	if got := transport.StopCalls(); got != 1 {
		t.Errorf("transport.StopCalls() = %d, want 1 (post-Send interruption must still clear queued audio)", got)
	}
	if got := transport.SentCount(); got != 1 {
		t.Errorf("transport.SentCount() = %d, want 1 (Send itself already completed before the race landed)", got)
	}
}

// blockingTTS blocks Synthesize until release is closed or ctx is
// cancelled, and signals entered exactly once, so a test can be certain
// interruption lands while synthesis is genuinely in progress.
type blockingTTS struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingTTS) Synthesize(ctx context.Context, _ string) ([]int16, int, error) {
	close(b.entered)
	select {
	case <-b.release:
		return []int16{0, 0}, 16000, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

// cancelOnSendTransport wraps fakeTransport and cancels an external
// context immediately after Send returns, deterministically simulating
// "interruption arrives right as delivery lands" without depending on
// goroutine scheduling.
type cancelOnSendTransport struct {
	*fakeTransport
	cancel context.CancelFunc
}

func (c *cancelOnSendTransport) Send(ctx context.Context, audio []int16, sampleRateHz int) error {
	err := c.fakeTransport.Send(ctx, audio, sampleRateHz)
	c.cancel()
	return err
}
