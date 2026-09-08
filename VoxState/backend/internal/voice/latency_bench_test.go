// M9 benchmarking: real, locally-measurable latencies for the
// interruption path. Every dependency here is an in-memory fake (no
// network, no real LiveKit/Rime/Deepgram) — these numbers measure this
// process's own goroutine-scheduling/lock-contention overhead, not
// anything network-bound. See RIME_EVIDENCE.md and the M9 benchmark
// report for what remains environment-dependent and unmeasured here.
package voice

import (
	"context"
	"testing"
	"time"

	"voxstate/backend/internal/agent"
	"voxstate/backend/internal/policy"
	"voxstate/backend/internal/state"
	"voxstate/backend/internal/tasks"
	"voxstate/backend/internal/tools"
)

// pollUntil is benchmark_test.go's equivalent of interruption_test.go's
// waitFor, taking testing.TB instead of *testing.T so it works from
// both benchmarks and tests.
func pollUntil(tb testing.TB, timeout time.Duration, cond func() bool) time.Duration {
	tb.Helper()
	start := time.Now()
	deadline := start.Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return time.Since(start)
		}
		time.Sleep(50 * time.Microsecond)
	}
	if !cond() {
		tb.Fatal("condition not met before timeout")
	}
	return time.Since(start)
}

func newBenchAgent(b *testing.B, started chan<- struct{}, finish <-chan struct{}) (*tasks.Store, *agent.Agent) {
	b.Helper()
	ss := state.NewStore()
	if _, _, _, err := ss.CreateMachine(state.CreateMachineInput{ID: "machine-17", Name: "Machine 17"}); err != nil {
		b.Fatalf("CreateMachine() error = %v", err)
	}
	ts := tasks.NewStore(ss, nil)
	ev := policy.NewEvaluator(ss)
	registry := tools.NewRegistry(
		repeatableFakeTool{name: tools.NameVibrationScan, started: started, finish: finish},
		tools.NewTemperatureScan(0),
	)
	ag := agent.New(ss, ts, ev, registry, nil)
	return ts, ag
}

// BenchmarkInterruptToStopAudio measures the time from an interrupting
// frame entering Session.Run's frame loop to Transport.StopAudio being
// called. This call is synchronous with frame handling (same goroutine,
// see handleFrame), so this is expected to be sub-millisecond — the
// benchmark exists to confirm that with a real measurement rather than
// assert it from reading the code.
func BenchmarkInterruptToStopAudio(b *testing.B) {
	for i := 0; i < b.N; i++ {
		started := make(chan struct{}, 1)
		finish := make(chan struct{})
		_, ag := newBenchAgent(b, started, finish)
		stt := &scriptedSTT{transcripts: []string{"check vibration", "check temperature"}}
		tts := &fakeTTS{}
		transport := newFakeTransport()
		session := NewSession("bench-session", "machine-17", "room-1", "user-1", ag, stt, tts, transport, nil)

		runCtx, cancel := context.WithCancel(context.Background())
		runErr := make(chan error, 1)
		go func() { runErr <- session.Run(runCtx) }()

		transport.frames <- loudFrame(16000, 16000)
		transport.frames <- quietFrame(16000, 16000)
		pollUntil(b, 2*time.Second, func() bool {
			select {
			case <-started:
				return true
			default:
				return false
			}
		})

		t0 := time.Now()
		transport.frames <- loudFrame(16000, 16000)
		transport.frames <- quietFrame(16000, 16000)
		elapsed := pollUntil(b, 2*time.Second, func() bool { return transport.StopCalls() >= 1 })
		_ = elapsed

		cancel()
		close(finish)
		<-runErr
		b.ReportMetric(float64(time.Since(t0).Microseconds()), "µs/interrupt→StopAudio")
	}
}

// BenchmarkInterruptToTaskCancelled measures the time from an
// interrupting frame to the interrupted turn's task actually being
// recorded CANCELLED by the task engine. Unlike StopAudio, this crosses
// a goroutine boundary: the interrupted turn's own goroutine has to
// observe ctx.Done() inside agent.Run's runTool and call
// tasks.Store.CancelTask itself (see agent.go) — this is the real
// asynchronous gap TestInterrupt_RapidDoubleInterruption's flake (found
// during M9 race hardening, see that test's updated comment) measures
// the size of.
func BenchmarkInterruptToTaskCancelled(b *testing.B) {
	for i := 0; i < b.N; i++ {
		started := make(chan struct{}, 1)
		finish := make(chan struct{})
		ts, ag := newBenchAgent(b, started, finish)
		stt := &scriptedSTT{transcripts: []string{"check vibration", "check temperature"}}
		tts := &fakeTTS{}
		transport := newFakeTransport()
		session := NewSession("bench-session", "machine-17", "room-1", "user-1", ag, stt, tts, transport, nil)

		runCtx, cancel := context.WithCancel(context.Background())
		runErr := make(chan error, 1)
		go func() { runErr <- session.Run(runCtx) }()

		transport.frames <- loudFrame(16000, 16000)
		transport.frames <- quietFrame(16000, 16000)
		pollUntil(b, 2*time.Second, func() bool {
			select {
			case <-started:
				return true
			default:
				return false
			}
		})

		t0 := time.Now()
		transport.frames <- loudFrame(16000, 16000)
		transport.frames <- quietFrame(16000, 16000)
		elapsed := pollUntil(b, 2*time.Second, func() bool {
			list, err := ts.ListTasksForMachine("machine-17")
			return err == nil && len(list) >= 1 && list[0].Status == tasks.StatusCancelled
		})
		_ = elapsed

		cancel()
		close(finish)
		<-runErr
		b.ReportMetric(float64(time.Since(t0).Microseconds()), "µs/interrupt→TaskCancelled")
	}
}
