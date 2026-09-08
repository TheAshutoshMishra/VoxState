# VoxState Roadmap

Ten milestones, M0 through M9. Each milestone should leave the system in a
runnable/demonstrable state for what it covers — no milestone should require
finishing a later one to make sense.

## M0 — Foundation & Architecture
**Objective:** Establish project structure, architecture, domain model,
event model, and roadmap so subsequent milestones have a shared reference.
**Not built yet:** Any Go code beyond `go.mod`, any database schema/
migrations, any frontend code, any LiveKit/Rime/LLM integration.

## M1 — Go Backend Skeleton
**Objective:** A runnable Go binary with the internal package layout wired
together (empty/stub implementations), basic HTTP server, health check,
config loading, and Postgres connection established.
**Not built yet:** Real state/event/task logic, any LLM or voice
integration, no actual diagnostic tools.

## M2 — Machine/Event/State Engine
**Objective:** Implement `Machine`, `StateVersion`, `MachineState`, and
`Event` persistence and the version-increment logic. Ability to create a
machine, change its state, and observe the version incrementing and events
being recorded.
**Not built yet:** Tasks, tool execution, agent/LLM reasoning, voice, policy
enforcement (there's nothing to enforce against yet without tasks).

## M3 — Task Engine & Cancellation
**Objective:** Implement `DiagnosticTask` creation bound to a state version,
task status transitions, and a working cancellation signal (context
cancellation) that simulated tools observe.
**Not built yet:** Real diagnostic tool logic (simulated/mocked tools only),
LLM-driven task creation (tasks are triggered manually/via test harness),
policy-based result rejection.

## M4 — Stale Result Protection
**Objective:** Implement the Policy layer: compare a `ToolResult`'s
`produced_for_state_version` against the machine's current version, accept
or reject, record `ToolResultRejected`/`ResponseInvalidated` events. This is
the milestone that proves the core claim of the project, independent of
voice.
**Not built yet:** LLM integration, voice transport — this is validated via
API calls / test harness first.

## M5 — Agent / Tool-Calling Loop
**Objective:** Build `internal/agent` (orchestration) and `internal/tools`
(diagnostic capability abstraction) so an instruction can flow end to end:
inspect machine state, select a tool via a narrow `Planner` interface,
create a task bound to the current version, run the tool, and gate the
result through `policy.Evaluator` before ever treating it as truth.
**Decision made during implementation:** M5 introduces the `Planner`
abstraction and a deterministic `KeywordPlanner` implementation, not a
wired-up LLM provider — no SDK, API key, or network call exists in
`internal/agent`. The `Planner` interface is what a real LLM would plug
into later; building that integration is deferred to whichever milestone
actually needs it, so it can be validated independently of an external
dependency. See `docs/ARCHITECTURE.md` and `CLAUDE.md`'s M5 entry for the
reasoning.
**Not built yet:** Realtime voice, an actual LLM provider, automatic
replanning after a rejection (M5 only signals that replanning is
required) — the agent is exercised via text/API first so the
orchestration loop is validated independent of voice transport concerns.

## M6 — Realtime Voice Integration
**Objective:** Integrate LiveKit for audio transport and Rime for TTS, so
the agent can be talked to and heard, using the text-based agent loop from
M5.
**Decisions made during implementation:** LiveKit's "Agents" framework
(the thing that would normally run STT/LLM/TTS orchestration inside a
room) is Python/Node.js only — no Go support exists or is planned. The Go
backend therefore joins LiveKit rooms directly as a raw participant via
`server-sdk-go/v2` + its `pkg/media` PCM track helpers, not via that
framework. STT was not pinned down in earlier milestones' docs; Deepgram
(official Go SDK, prerecorded REST endpoint called once per
locally-segmented utterance) was selected during M6. Rime has no official
SDK in any language except Python framework plugins, so `internal/voice/rime`
is a hand-rolled REST client. See `docs/ARCHITECTURE.md` and `CLAUDE.md`'s
M6 entry for the full reasoning.
**Not built yet:** Interruption handling (the agent can be talked to, but
talking over it doesn't yet do anything special — inbound audio is simply
dropped while a response is being spoken), frontend, production-grade
voice activity detection (a fixed energy/silence heuristic segments
utterances), database/event persistence for `VoiceSession`s (in-memory
only, same as every other engine today).

## M7 — Voice Interruption & Recovery
**Objective:** Detect user interruption via LiveKit, trigger task
cancellation (M3) and response invalidation, and have the agent replan and
respond based on current state. This is where M3/M4/M6 come together.
**Decisions made during implementation:** every instruction is now a
logical **turn** (`internal/voice`'s unexported `turn` type — an ID plus
the `context.CancelFunc` for that turn's own child context). `Session.Run`
still owns segmentation on a single goroutine, but each complete utterance
now starts its turn on its own goroutine, so the frame loop keeps
consuming inbound audio (and can therefore detect a barge-in) while a turn
is still being processed or spoken. A loud inbound frame arriving while a
turn is active is treated as interruption: the turn's context is
cancelled — reusing the exact M3/M5 cancellation path `agent.Run`/
`tasks.Store` already had (no second cancellation mechanism) —
`voice.Transport` gained one new method, `StopAudio`, so already-queued
outbound audio can be discarded promptly (necessary because `Transport.Send`
only enqueues for asynchronous, real-time-paced delivery — see
`voice/livekit`'s `Transport.Send`/`lkmedia.PCMLocalTrack` — so cancelling
a context after `Send` returns does nothing on its own). `internal/events`
gained the two event types `docs/EVENT_MODEL.md` had already reserved for
this milestone, `UserInterrupted` and `ResponseInvalidated`; both are
logged (no persistent store yet, same M2–M6 limitation) with
session/turn/machine identifiers. Stale-result fencing was **not**
duplicated into voice: `handleUtterance` checks `ctx.Err()` before every
user-visible side effect purely as a turn-ownership guard, never comparing
state versions itself — `policy.Evaluator` remains the sole authority on
that. A true simultaneous tie between "the tool finished" and "the user
interrupted" is resolved the same way M3's own `CancelTask`-vs-`finishTask`
race already is (whichever wins is fine, as long as the outcome is single
and consistent) — see `TestRace_InterruptVsToolCompletion`'s doc comment
in `internal/voice/interruption_test.go` for why guaranteeing the
interrupt always wins that exact tie would require holding a session-wide
lock across TTS synthesis, which would block barge-in detection during
every response.
**Not built yet:** Frontend visualization of any of this — still
API/log-observable only. Production-grade VAD/barge-in detection is still
M6's fixed energy/silence heuristic (`segmenter.loud`), not adaptive to
ambient noise or false positives.

## M8 — Frontend & Visualization
**Objective:** Next.js/React frontend showing machine state, live event
timeline, active/cancelled tasks, and rejected results, so the mechanics
from M2–M7 are visible to an audience in realtime, not just in logs.
**Decisions made during implementation:** the frontend became a real
control surface, not just a read-only viewer — every panel (machine,
state timeline, tasks, policy, voice, activity) both displays backend
state and drives it through the existing HTTP API, since a click-through
demo needs to *cause* the version bump and stale result, not just
observe one that already happened. `PolicyPanel` never recomputes
ACCEPTED/REJECTED itself; it only renders whatever `POST
/tasks/{id}/result` returns, and includes a one-click "Guided Stale-
Result Demo" that reproduces the project's core scenario end to end. A
small additive backend piece, `internal/activity` (a bounded in-memory
ring buffer wrapping the shared `*slog.Logger`), plus `GET /activity`,
was added so background-goroutine events (task lifecycle, M7 voice turn
events) that have no HTTP caller to hand them to are still visible to
the frontend — no existing package's method signatures changed. A second
binary, `cmd/devserver`, wires the identical stack as `cmd/server` with
in-memory LiveKit/Rime/Deepgram fakes so the frontend is developable and
demoable without `libopus`/`pkg-config` or real voice credentials.
See `CLAUDE.md`'s M8 entry for the full breakdown.
**Not built yet:** Any change to backend domain logic (state/task/policy
engines untouched) or database/event persistence (still in-memory) —
this milestone consumes what already exists and adds one small, additive
observability seam (`internal/activity`) to expose it.

## M9 — Testing, Benchmarking & Demo
**Objective:** End-to-end test coverage of the stale-result and
interruption flows, basic latency/timing benchmarks for the
interrupt→cancel→replan loop, and a rehearsed demo script covering the
scenario in the project brief (Machine 17 example).
**Decisions made during implementation:** a repeated-run stress pass
(`go test -race -count=20..50`) on the concurrency-critical packages
found one genuine test flake, `TestInterrupt_RapidDoubleInterruption` —
root-caused to a missing wait for an async task-cancellation goroutine,
not a production bug — fixed in place rather than papered over. Added
three `_test.go`-only benchmark files (`internal/policy`, `internal/tasks`,
`internal/voice`) measuring what's actually measurable locally (policy
decision cost, task create/cancel latency, interrupt→StopAudio and
interrupt→task-cancelled latency); real network-bound metrics (Rime/
LiveKit/Deepgram) are explicitly marked not-measured in `BENCHMARKS.md`
rather than estimated. `RIME_EVIDENCE.md` documents the Rime integration
from actual source values and discloses one real gap found in the
process (`rime.Config.Lang` is never set). `DEMO.md` is the rehearsed
demo script. See `CLAUDE.md`'s M9 entry for the full breakdown.
**Not built yet:** N/A — this milestone hardens and demonstrates what
exists; no new product surface is introduced. This is the final
milestone (no M10).
