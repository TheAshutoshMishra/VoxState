# M9 benchmarks & race-hardening results

All numbers below were measured in this development environment
(`AMD Athlon PRO 3045B`, 2 logical CPUs, WSL2/Linux) on 2026-09-08 by
actually running the commands shown — none are estimated or invented.
Every scenario uses in-memory fakes (no real network, no real LiveKit/
Rime/Deepgram) — see `RIME_EVIDENCE.md` for what remains genuinely
untested against live third-party services.

## Race / concurrency verification

```
cd backend
go test -race $(go list ./... | grep -v voice/livekit | grep -v cmd/server)
```
Result: **all packages pass** (`activity`, `agent`, `api`, `config`,
`events`, `policy`, `server`, `state`, `tasks`, `tools`, `voice`,
`voice/deepgram`, `voice/rime`). `internal/voice/livekit` and
`cmd/server` do not build in this environment — missing `libopus`/
`pkg-config` system dependency, unrelated to any test.

Repeated-run stress pass (`go test -race -count=N`), to catch
intermittent races a single run wouldn't:

| Package | Runs | Result |
|---|---|---|
| `internal/state` | 20 | pass |
| `internal/tasks` | 20 | pass |
| `internal/policy` | 20 | pass |
| `internal/agent` | 20 | pass |
| `internal/voice` | 30 (+ 50 targeted at the flake below) | pass |

**One genuine flake was found and fixed** during this pass:
`TestInterrupt_RapidDoubleInterruption` failed intermittently (observed
once in an initial 20-run sweep) with `task B status = "RUNNING", want
CANCELLED`. Root cause (confirmed by reading `agent.go`'s `runTool`):
task cancellation on interrupt happens on the interrupted turn's own
goroutine (`agent.Run` observes `ctx.Done()` and calls
`tasks.Store.CancelTask` itself), asynchronously relative to the
*next* turn's TTS call — the test synchronized on "next turn spoke" but
asserted on "previous turn's task is cancelled" with no ordering
guarantee between the two. This is a test-synchronization gap, not a
production bug: the guarantee actually being tested (stale content never
reaches TTS) was independently verified and never violated. Fixed by
adding an explicit wait for the task's status to leave `RUNNING` before
asserting on it (`internal/voice/interruption_test.go`), mirroring the
pattern the rest of the suite already uses. Re-verified stable at
**50/50** repeated `-race` runs after the fix.

Required race scenarios and their covering tests (all pre-existing,
verified still passing, not rewritten):

| Scenario | Test |
|---|---|
| state change vs. task creation | `TestChangeState_ConcurrentUpdatesProduceUniqueSequentialVersions`, `TestCreateTask_BoundVersionSurvivesMachineChange` |
| task completion vs. cancellation | `TestCompletionVsCancellationRace`, `TestConcurrentCancellation_SingleWinnerNoRace` |
| tool completion vs. interruption | `TestRace_InterruptVsToolCompletion` |
| interruption vs. response delivery | `TestRace_InterruptVsAudioDelivery` |
| interruption vs. TTS | `TestRace_InterruptVsTTSGeneration` |
| stale result vs. state change | `TestStaleResultRejectedAfterStateChange`, `TestAgentRejectsStaleResult`, `TestSessionNeverSpeaksStaleResult` |

## Latency benchmarks

New benchmark files added for M9 (all `_test.go`, zero production code
changes): `internal/policy/evaluator_bench_test.go`,
`internal/tasks/store_bench_test.go`,
`internal/voice/latency_bench_test.go`.

```
go test -run '^$' -bench . -benchtime=200x \
  ./internal/policy/ ./internal/tasks/ ./internal/voice/
```

| Benchmark | Result (range across repeated runs) | What it measures |
|---|---|---|
| `BenchmarkEvaluate_Accepted` | 260-400 ns/op | `policy.Evaluator.Evaluate`, matching version — the version-fencing decision itself |
| `BenchmarkEvaluate_RejectedStale` | 3.1-5.1 µs/op | same, stale version — slower because a real `events.Event` (with a generated ID) is constructed only on rejection |
| `BenchmarkCreateTask` | 4.1-5.8 µs/op | task creation + version binding |
| `BenchmarkCancelTask_Pending` | 1.9-2.1 µs/op | `CancelTask`'s own synchronous status flip |
| `BenchmarkInterruptToStopAudio` | 139-245 µs (custom metric, non-`-race`) | wall time from an interrupting frame entering `Session.Run` to `Transport.StopAudio` being called (same goroutine as the frame loop) |
| `BenchmarkInterruptToTaskCancelled` | 153-230 µs typical, one outlier run at ~1.3 ms observed (custom metric, non-`-race`) | wall time from the same interrupt to the *previous* turn's task actually reaching `CANCELLED` — crosses a goroutine boundary (see the flake analysis above); this is the real size of that async gap |

**Methodology note:** every number above is a range across 3-4 repeated
`-benchtime=200x` (or `100x` for `internal/voice`) invocations on this
shared, 2-logical-CPU VM, not a single run. An earlier draft of this
file reported single-point numbers from one initial run
(`BenchmarkCreateTask` at 58.9 µs/op, `BenchmarkCancelTask_Pending` at
20.6 µs/op) that turned out to be ~10x slower than every subsequent
re-run under quieter system load — almost certainly contention from
other processes on this machine at that moment, not a real
characteristic of the code. Corrected here rather than left standing,
since a single unrepresentative run is exactly the kind of number this
document is supposed to not contain. Re-run yourself with:
```
go test -run '^$' -bench . -benchtime=200x ./internal/policy/ ./internal/tasks/
go test -run '^$' -bench 'BenchmarkInterrupt' -benchtime=100x ./internal/voice/
```

**Not measured (environment-dependent, requires deployed
LiveKit/Rime/Deepgram):** real network RTT to Rime/Deepgram, real
LiveKit media pipeline latency, end-to-end mic-to-speaker interruption
latency with a real participant. See `RIME_EVIDENCE.md` for the full,
honest boundary of what was and wasn't exercised against live services.

## Stale-result / cancellation behavior (live, not just unit-tested)

Executed against `go run ./cmd/devserver` (identical HTTP API to
`cmd/server`, in-memory voice fakes) — see the full transcript in the
M9 verification report. Summary:

- Task bound to v1 → machine changed to v2 → task's result submitted →
  `{"outcome":"REJECTED","reason":"result bound to state version 1, but
  machine is now at version 2: result is stale"}`.
- Agent run with a 3-second tool delay, machine changed mid-flight →
  `{"outcome":"REJECTED","replan_required":true}` with **no `payload`
  field present at all** — nothing for a spoken response to draw from.
- Same agent run, machine left untouched → `{"outcome":"ACCEPTED",
  "payload":{"vibration":"NORMAL"}}`.
