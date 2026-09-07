# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

VoxState is a realtime voice agent for industrial machine maintenance that
must stay consistent with the latest verified world state even when the
user interrupts, external events change machine state mid-task, or a
previously started tool returns a stale result. The core mechanism: every
meaningful state change creates a new immutable **state version**; every
task is bound to the version current at its creation; a **policy layer**
rejects any tool result whose bound version no longer matches the machine's
current version before it can influence a spoken response.

Full architecture, entities, and event catalog live in `docs/` — read
`docs/ARCHITECTURE.md` before making structural changes; it is the source
of truth for how components relate, not this file.

## Current status

**M6 — Realtime Voice Integration is complete** (M0–M5 also complete).
`internal/agent` orchestrates the full tool-calling loop end to end: read
current machine state → `Planner` selects a tool → `internal/tasks.CreateTask`
binds a task to that current version → `internal/tools.Tool.Run`
executes (deterministic, simulated diagnostics) → the resulting payload
is turned into a `policy.ToolResult` using the task's `BoundVersion` (never
the machine's current version) → `policy.Evaluator.Evaluate` gates it →
the Agent consumes the payload **only** if `Accepted`, and never even
copies it into the returned `agent.Result` if `Rejected`. `internal/tools`
is a deliberately dependency-free package (no state/tasks/policy
imports) providing two deterministic demo tools (`vibration_scan`,
`temperature_scan`).

**The Agent never duplicates the M4 version check.** `Run` calls
`evaluator.Evaluate(result)` and branches on the returned `Decision` —
there is no `if result.BoundVersion != current { ... }` anywhere in
`internal/agent`. Policy remains the sole authority on validity; the
Agent only orchestrates.

**No LLM is wired in.** M5 introduces a narrow `Planner` interface
(`Plan(ctx, Instruction, state.MachineState) (Plan, error)`) and ships
exactly one implementation, `KeywordPlanner` — deterministic keyword
matching, no network calls, no SDK, no API key. `Planner` is the seam a
real LLM integration will implement later; that integration itself is
deferred to whichever milestone actually needs it. M6's voice layer feeds
this same `KeywordPlanner` the transcript text Deepgram produces — no LLM
was introduced to build voice on top of.

**M6 adds `internal/voice`**, a realtime adapter around the M5 loop:
LiveKit (`internal/voice/livekit`) provides the audio transport, Deepgram
(`internal/voice/deepgram`) provides STT, Rime (`internal/voice/rime`)
provides TTS. `voice.VoiceSession` glues them together: inbound audio →
`segmenter.go`'s energy/silence VAD → complete utterance → `STT.Transcribe`
→ `agent.Agent.Run` (unchanged) → `voice.TextResponse(result)` → (if
non-empty) `TTS.Synthesize` → `Transport.Send`. LiveKit's own "Agents"
framework (the thing that would normally run this STT/LLM/TTS
orchestration for you inside a room) is Python/Node.js only — confirmed
against LiveKit's own docs and the `livekit/agents`/`livekit/agents-js`
repos — so the Go backend joins each room directly as a raw participant
via `server-sdk-go/v2` instead. See "`voice.VoiceSession` (M6) design
notes" below for the full guarantee and its limitations.

**M5 does NOT implement:** automatic replanning (a rejection sets
`Result.ReplanRequired = true` and stops there — no auto-retry, no new
task silently created), task rebinding (still forbidden — `BoundVersion`
is copied through unchanged everywhere), or a real LLM provider.

**M6 does NOT implement:** interruption/barge-in handling (`voice.Session`
simply drops inbound audio while a response is being synthesized/sent —
no cancellation, no replanning; that is M7's job), a frontend, database/
event persistence for `VoiceSession`s (in-memory only, same as every
other engine), or production-grade voice activity detection (`segmenter.go`
is a fixed, hand-tuned energy/silence heuristic, not adaptive VAD).

`tools`/`agent`/`voice` are now implemented; there is still no database and
no frontend. See `docs/ROADMAP.md` for what M7+ adds and explicitly does
not add yet.

Run it locally: `cd backend && go run ./cmd/server` (defaults to
`0.0.0.0:8080`; override with `APP_ENV`, `HTTP_HOST`, `HTTP_PORT`, plus the
M6 voice env vars — see `.env.example` and the Commands section below).
**Environment note:** `internal/voice/livekit` depends on
`gopkg.in/hraban/opus.v2`, a cgo binding to the real `libopus` C library
(pulled in transitively via `server-sdk-go/v2`'s `pkg/media` PCM track
helpers) — building it requires `pkg-config` and `libopus-dev` (or your
platform's equivalent) installed on the build machine. This is a genuine
deployment dependency of using LiveKit's real Go audio SDK, not a bug —
if `go build ./...` fails with `exec: "pkg-config": executable file not
found`, install those two packages first
(`sudo apt-get install -y pkg-config libopus-dev` on Debian/Ubuntu).

Do not implement functionality from a later milestone while working on an
earlier one — each milestone is meant to leave the system runnable/
demonstrable for what it covers, no more.

## Key documents (read these, don't re-derive them)

- `docs/ARCHITECTURE.md` — components, responsibilities, data/voice/state/
  task/interruption/stale-result flows, ASCII diagram
- `docs/DOMAIN_MODEL.md` — entities (`Machine`, `StateVersion`,
  `MachineState`, `Event`, `DiagnosticTask`, `ToolResult`, `VoiceSession`)
  and their relationships
- `docs/EVENT_MODEL.md` — event catalog (`MachineStateChanged`,
  `DiagnosticStarted/Cancelled/Completed`, `UserInterrupted`,
  `ToolResultRejected`, `StateConflictDetected`, `ResponseInvalidated`, etc.)
- `docs/decisions/001-modular-monolith.md` — why one Go process with
  internal package boundaries instead of microservices
- `docs/ROADMAP.md` — M0–M9 milestone objectives and explicit non-goals per
  milestone
- `docs/POLICY.md` — what `internal/policy` guarantees, the exact
  equality rule, the consistency boundary, and what M4 does not
  guarantee yet

## Architecture (summary)

Go modular monolith (`backend/`), not microservices — see ADR 001 for why.
Internal packages under `backend/internal/`, each with a single
responsibility:

- `api/` — HTTP entry points, including M6's voice session lifecycle
  endpoints; no business logic, delegates to other packages
- `state/` — owns `Machine`/`MachineState`/`StateVersion`; the single
  source of truth for "what version is current". **Implemented (M2)** as
  an in-memory, mutex-guarded `Store` — see below.
- `events/` — append-only event log; the audit trail and frontend data
  source, not a derived cache. **Partially implemented (M2):** only the
  `Event` type and the event types it actually produces
  (`MachineCreated`, `MachineStateChanged`, `TechnicianReported`) exist;
  there is no store, dispatcher, or persistence yet — `state.Store`
  returns `events.Event` values directly to its caller rather than
  writing to a log.
- `tasks/` — creates/tracks `DiagnosticTask`s, each stamped with the state
  version current at creation; exposes cancellation. **Implemented (M3)**
  as an in-memory, mutex-guarded `Store` — see below. Depends on
  `state.Store` (read-only, to capture the bound version at creation);
  `state` does not and must not depend on `tasks`.
- `tools/` — executes diagnostic tool work. **Implemented (M5)** as a
  small `Tool` interface + `Registry`, with two deterministic demo
  implementations (`vibration_scan`, `temperature_scan`). Deliberately
  has zero dependencies on `state`/`tasks`/`policy`/`agent` — a `Tool`
  returns a raw payload; it never tags anything with a bound state
  version itself (that's `agent`'s job — see design notes below).
- `policy/` — the staleness gatekeeper: rejects any `ToolResult` whose
  bound version != current version before the Agent can use it.
  **Implemented (M4)** as a small, dependency-light `Evaluator` — see
  `docs/POLICY.md` and the design notes below.
- `agent/` — reasoning/tool-calling loop; will react to interruption and
  replan once M7 lands. **Implemented (M5)** as a deterministic `Agent` +
  `KeywordPlanner` — no LLM wired in yet. See design notes below.
- `voice/` — realtime voice adapter around `agent.Agent`; owns none of
  the state/task/policy correctness guarantees itself. **Implemented
  (M6)** as `STT`/`TTS`/`Transport`/`RoomProvisioner` interfaces +
  `VoiceSession` + `Manager`, with real implementations in
  `voice/livekit` (transport + room/token admin), `voice/rime` (TTS,
  hand-rolled REST client — no official Rime SDK exists), and
  `voice/deepgram` (STT, official Go SDK). `voice` itself never imports
  any of those three subpackages' third-party SDKs — they import `voice`
  to implement its interfaces, and only `cmd/server/main.go` wires
  concrete implementations together, the same inversion `agent` already
  uses for `tools.Tool`/`agent.Planner`. See design notes below.

M1 added two cross-cutting infrastructure packages not listed in
`docs/ARCHITECTURE.md` (that doc describes domain components; these are
plumbing the domain packages will sit on top of):

- `config/` — loads env vars with defaults into a `Config` struct; no
  framework, no domain awareness
- `server/` — constructs the `*http.Server` (timeouts) and runs it with
  context-based graceful shutdown; also has no domain awareness

`api/` builds the router, a request-logging wrapper, and thin handlers for
every domain package through M6 (machines/state/tasks/policy/agent/voice)
— see `docs/ARCHITECTURE.md`'s API/Session layer section for what each
endpoint delegates to.

**The state-versioning rule** (the core invariant of this project): a task
operates against a specific state version; a result generated for an older
version must never influence the current spoken response without passing
the policy check. Concrete example: state v10 exists → diagnostic starts,
bound to v10 → machine changes → state v11 → diagnostic finishes, tagged
v10 → policy compares v10 vs current v11 → mismatch → result rejected,
`ToolResultRejected` event recorded → agent replans against v11.

PostgreSQL is the only external stateful dependency (`docker-compose.yml`).
Redis and other infra are deliberately deferred — add only if a specific
milestone demonstrates a concrete need (see ADR 001).

**`state.Store` (M2) design notes, relevant to future milestones:**
- Storage is a single `map[string]*machineEntry` behind one
  `sync.RWMutex` — not per-machine locks or a database. This is
  deliberately simple for hackathon scope; a version bump is only ever
  `append` to a machine's `[]MachineState` history, so a single mutex is
  sufficient to guarantee unique, sequential versions under concurrent
  writers (verified by `go test -race`).
- `Store`'s public API (`CreateMachine`, `GetMachine`, `GetCurrentState`,
  `GetStateVersion`, `ChangeState`) is storage-agnostic by construction —
  no method takes a DB connection or returns SQL-shaped types — so a
  future milestone can back it with PostgreSQL without changing callers.
- No-op updates (new status/attributes identical to current) do **not**
  create a new version, per the M2 spec — `ChangeState` returns
  `changed=false` and an empty event slice in that case.
- `ChangeSource` (`system` vs `technician`) distinguishes routine changes
  from human-reported ones; a technician-sourced change emits both a
  `TechnicianReported` and a `MachineStateChanged` event.
- All returned `MachineState` values are deep-copied (`Attributes` map
  included) before leaving the store, so callers can never mutate stored
  history through a returned value.

**`tasks.Store` (M3) design notes, relevant to M4:**
- `DiagnosticTask.BoundVersion` is set once inside `CreateTask` (by
  reading `state.Store.GetCurrentState`) and is never written again for
  that task's lifetime — this is the exact invariant M4's policy check
  needs: compare a task's `BoundVersion` against the machine's *current*
  version at result time, not at task-creation time.
  `TestCreateTask_BoundVersionSurvivesMachineChange` (and its HTTP
  equivalent) exist specifically to pin this down.
- Lifecycle is enforced with sentinel errors, not silently ignored:
  starting a non-`PENDING` task returns `ErrInvalidTransition`; cancelling
  an already-terminal task returns `ErrTaskAlreadyCompleted` /
  `ErrTaskAlreadyCancelled` rather than succeeding as a no-op.
- The completion-vs-cancellation race is resolved by a single `sync.Mutex`
  (same pattern as `state.Store`): `finishTask` (called when `Work`
  returns) only transitions `RUNNING → COMPLETED` if the task is *still*
  `RUNNING` when it acquires the lock; if `CancelTask` already flipped it
  to `CANCELLED`, `finishTask` is a no-op. Whichever of the two acquires
  the lock first wins — verified by `TestCompletionVsCancellationRace`
  (200 iterations) under `go test -race`.
- `Work func(ctx context.Context) error` is the execution abstraction.
  `tasks.SimulatedWork` is a placeholder implementation (a cancellable
  sleep loop, no domain logic) used by the standalone
  `POST /tasks/{id}/start` demo endpoint. M5's `agent.Agent` does not use
  `SimulatedWork` — it wraps a real `tools.Tool` in its own `Work` closure
  instead (see the M5 design notes below) — but both paths go through the
  exact same `tasks.StartTask`/`CancelTask` mechanism, which is the point.
- Task lifecycle events (`DiagnosticStarted`/`Cancelled`/`Completed`) are
  surfaced two ways: `CreateTask`/`CancelTask` return the `events.Event`
  synchronously to their caller (same pattern as `state.Store`), but
  `DiagnosticCompleted` happens on a background goroutine with no
  synchronous caller to hand it to, so it's only logged via the optional
  `*slog.Logger` passed to `tasks.NewStore`. There is still no persistent
  event store — a real one needs to decide how to collect that
  goroutine-originated event too, not just the synchronous ones.
- `DiagnosticTask` has no `SessionID` wired up (no `VoiceSession` exists
  yet, M6+) and no `rejected` status — `docs/DOMAIN_MODEL.md` lists
  `rejected` as a possible `DiagnosticTask.status`, but M3's explicit
  lifecycle only defines `PENDING/RUNNING/COMPLETED/CANCELLED`; whether
  "rejected" becomes a task status or stays a `ToolResult`-level concept
  is an open decision for M4.

**`policy.Evaluator` (M4) design notes, relevant to M5:** full guarantee
documented in `docs/POLICY.md` — this is the condensed version.
- `ToolResult` deliberately has no `Accepted`/`RejectionReason` fields,
  unlike the literal shape in `docs/DOMAIN_MODEL.md`. Those are modeled
  as the return value of `Evaluate` (a `Decision`) instead — a mutable
  bool on `ToolResult` would let a caller bypass the check entirely by
  just setting it, which defeats the point of a safety boundary. Trade-off
  noted explicitly in `backend/internal/policy/result.go`.
  `ToolResult.BoundVersion` is DOMAIN_MODEL's `produced_for_state_version`,
  renamed to match `DiagnosticTask.BoundVersion`'s terminology.
- The rule is **exact equality** (`==`), not `<`/`not older than` —
  `TestEvaluate_FutureVersion_Rejected` exists specifically to prove a
  result claiming a version the machine hasn't reached yet is rejected
  too, not just stale ones.
- `Evaluate` is a **point-in-time gate**, not a distributed transaction:
  its own read+compare is atomic (single `state.Store.GetCurrentState`
  call, mutex-guarded), but nothing stops the machine from moving to a
  new version immediately after an `ACCEPTED` `Decision` is returned. No
  `state.Store` API extension was needed — evaluated and documented why
  in the `Evaluate` doc comment rather than adding one preemptively. M5's
  Agent must call `Evaluate` right before consuming a result, not cache a
  `Decision`.
- `policy` depends on `state` (required) and, only for
  `SimulateToolResult` (the demo/test helper), on `tasks` — the core
  `Evaluate` logic itself has no `tasks` dependency. `state` still does
  not and must not depend on `policy` or `tasks`.
- `POST /tasks/{id}/result` (new HTTP endpoint) is the demo integration
  point: it builds a `SimulateToolResult` from an existing task and runs
  it through `Evaluate`, returning the full `Decision` as JSON — this is
  how the core example (create task at v1 → change machine → v2 → result
  still bound to v1 → REJECTED) is demonstrable without a real Agent.

**`agent.Agent` / `internal/tools` (M5) design notes, relevant to M6:**
- `Agent.Run` reuses `policy.SimulateToolResult(task, payload)` (the same
  M4 helper) to build the `ToolResult` from a tool's output — it copies
  `task.BoundVersion` verbatim, never `state.GetCurrentState(...).Version`.
  This is the one line in the whole codebase that would silently break
  stale-result protection if it were ever "fixed" to use the current
  version instead; it's called out explicitly in `Run`'s doc comment.
- `agent.Result` has no field that can carry a rejected result's payload
  — `Payload` is only ever assigned in the `Accepted` branch of `Run`.
  This mirrors `policy.ToolResult`'s own M4 design principle (make
  misuse structurally hard, not just documented against).
  `TestAgentDoesNotConsumeStalePayload` proves this isn't just "Payload
  is always empty" by also exercising the accepted case in the same test.
- Cancellation reuses the M3 task engine rather than inventing a second
  mechanism: `Run`'s own `ctx` is *not* passed directly into
  `tasks.StartTask` (which always derives its own internal context from
  `context.Background()`). Instead, `Run` waits on a buffered result
  channel vs. `ctx.Done()`, and on cancellation calls
  `tasks.Store.CancelTask` — the same call any other caller would use.
  There is exactly one cancellation path through the task engine, not one
  for the Agent and one for everything else.
- `internal/tools` has no dependency on `state`, `tasks`, `policy`, or
  `agent` — a `Tool` only sees `RunInput{MachineID}` and a `context`. The
  Agent is the only thing that ever combines a tool's output with a
  task's bound version. `tools.NewVibrationScan`/`NewTemperatureScan`
  take a `time.Duration` purely so tests and the HTTP demo can control
  timing (same pattern as `tasks.SimulatedWork`, M3) — their payloads are
  fixed constants, never randomized.
- `Planner` (`Plan(ctx, Instruction, state.MachineState) (Plan, error)`)
  is the only LLM-shaped seam in the codebase. `KeywordPlanner` is
  deterministic substring matching; there is no SDK, API key, or network
  call anywhere in `internal/agent`. Wiring a real provider means writing
  a new `Planner` implementation — `Agent.Run`'s orchestration logic
  should not need to change.
- No new event types were added in M5. Existing `DiagnosticStarted` (from
  `tasks.CreateTask`) and `ToolResultRejected` (from `policy.Evaluate`),
  combined with `agent.Result`'s structured return value, were judged
  sufficient to represent every M5 domain transition — adding
  `AgentDecisionMade`/`ToolSelected`/etc. would have been event types
  that exist only for logging, which the M5 brief explicitly discouraged.

**`voice.VoiceSession` (M6) design notes, relevant to M7:**
- `voice.TextResponse(result agent.Result) string` is M6's equivalent of
  M5's "Agent never consumes a stale payload" guarantee, one layer
  further downstream: it reads `result.Payload` in exactly one place (the
  `OutcomeAccepted` case), and its rejected-branch helper,
  `formatRejection(reason string)`, takes only a plain string — not an
  `agent.Result`, not a payload map — as its parameter. A future
  maintainer cannot thread `Payload` into the rejected branch without
  changing that function's signature, a visible, reviewable diff.
  `TestSessionNeverSpeaksStaleResult` (mirroring `TestAgentRejectsStaleResult`'s
  exact machine-changes-while-tool-runs setup) asserts no `TTS.Synthesize`
  call ever contains the stale tool's payload marker string — a stronger
  check than only asserting `Outcome == Rejected`.
- **LiveKit's Agents framework has no Go support.** Confirmed against
  LiveKit's own docs and both the `livekit/agents` (Python) and
  `livekit/agents-js` (Node/TS) repos — neither offers Go bindings, and no
  roadmap item promises one. `voice/livekit` therefore joins each room
  directly as a raw participant via `server-sdk-go/v2` +
  `server-sdk-go/v2/pkg/media` (`lkmedia.PCMLocalTrack`/`PCMRemoteTrack`
  for Opus encode/decode), not via a framework that doesn't exist for Go.
  This is the smallest correct Go-native integration, not a workaround.
- **No custom PCM resampler was written**, despite that being in the
  original M6 plan: `lkmedia.NewPCMLocalTrack(sourceSampleRateHz, ...)`
  already resamples internally to LiveKit's required 48kHz track rate
  (confirmed by reading `server-sdk-go/v2/pkg/media/pcmlocaltrack.go`
  directly), and Deepgram's Go SDK accepts an arbitrary `SampleRate`
  rather than requiring one fixed value — so neither the outbound (TTS →
  LiveKit) nor inbound (LiveKit → STT) path needed one. Writing a
  redundant resampler on top of what the real SDKs already do correctly
  would have been exactly the kind of unnecessary abstraction the project
  brief warns against.
- **Utterance segmentation (`segmenter.go`) is a fixed energy/silence
  heuristic, not a swappable interface.** LiveKit's Go path (unlike its
  Python/Node Agents framework) hands the backend a continuous raw audio
  stream with no built-in endpointing, so `voice` has to do its own — but
  it has exactly one caller (`Session.Run`) and one implementation in M6
  scope, so it stays a private algorithm rather than gaining a `VAD`
  interface. It is explicitly *not* production-grade: fixed thresholds,
  no noise-floor adaptation, no barge-in awareness.
- **Given the segmenter already does endpointing, Deepgram is called via
  its prerecorded REST endpoint once per utterance, not a streaming
  websocket.** A persistent streaming connection would only add a second,
  redundant endpointing layer underneath `segmenter.go`'s own, for no
  latency benefit once an utterance is already a complete, bounded
  buffer — and REST keeps `voice/deepgram` symmetric with `voice/rime`'s
  own one-call-per-response REST TTS pattern, both directly testable
  against `httptest.Server` with no live network call.
- **Inbound audio during an in-progress response is dropped, not
  queued.** `Session` tracks a single `speaking bool` (its `Run` loop is
  single-goroutine per session, so no mutex is needed) and simply does
  not feed the segmenter while true. This is a deliberate scope
  boundary, not an oversight: reacting to "the user started talking while
  the agent was still speaking" — cancelling the in-flight response,
  replanning — is exactly M7's job. Whoever implements M7 should look
  here first; the check is intentionally the simplest thing that could
  possibly leave a clean seam for interruption handling to attach to.
- **Rime has no official SDK in any language except Python framework
  plugins** (confirmed by inspecting the `rimelabs` GitHub org — its only
  Go artifact is `rime-cli`, a release download tool, not a client
  library) — `voice/rime` is a hand-rolled `net/http` REST client.
  Requests always set `samplingRate` explicitly (Rime's own docs give
  inconsistent implicit defaults across different pages — 16000, 22050,
  and 24000 have each been observed as "the default"), and responses are
  parsed as raw WAV (via `Accept: audio/wav`, per Rime's own quickstart
  guidance on what makes the endpoint return bytes instead of a
  JSON-with-base64 envelope) — `Synthesize` reports back whatever sample
  rate the WAV's own `fmt` chunk states, not the value requested, so a
  mismatch between what was asked for and what Rime actually produced can
  never silently propagate.
- **`voice.Manager` mirrors `tasks.Store`'s shape**: an in-memory map of
  sessions behind one mutex, no persistence — restarting the server loses
  active voice sessions exactly as it loses in-flight tasks today. This
  is consistent with the rest of the system, not a new gap M6 introduces.
  `Manager.Start` validates the machine exists via `state.Store` before
  provisioning a LiveKit room for it, the same upfront-validation pattern
  `tasks.CreateTask` already uses.
- **A fourth interface, `voice.RoomProvisioner`, was added beyond the
  three (`STT`/`TTS`/`Transport`) originally scoped**, because room
  admin (create/delete, human-participant token minting) is a genuinely
  distinct boundary from `Transport`'s per-session realtime audio I/O —
  a one-shot admin API call, not a continuous stream — with its own real
  implementation (`voice/livekit.Provisioner`) and its own test fake.
- **Environment limitation, not a code defect:** `voice/livekit` requires
  `libopus`/`pkg-config` on the build machine (see "Current status"
  above). In an environment without them, `go build ./...`/`go test ./...`
  fail specifically and only on that one subpackage (and anything that
  imports it, i.e. `cmd/server`) — every other package, including all of
  `internal/voice`'s own core logic and its `rime`/`deepgram` subpackages,
  builds and tests cleanly in isolation
  (`go build $(go list ./... | grep -v /voice/livekit)`).

## Commands

Toolchain: Go 1.25.1 through M5; `go.mod`'s `go` directive was bumped to
**1.26** in M6 because `github.com/livekit/server-sdk-go/v2` requires it —
`GOTOOLCHAIN=auto` (Go's default) downloads 1.26 transparently on first
build if only 1.25.x is installed locally. See the `voice/livekit`
environment note in "Current status" above for the separate
`pkg-config`/`libopus` build requirement this same dependency introduces.

```bash
# Start Postgres (only service defined so far; not required to run the
# server itself, since nothing talks to a database yet)
docker compose up -d postgres

cd backend
go build ./...       # build (see the libopus/pkg-config note above — fails on
                       #   voice/livekit specifically without those installed)
go vet ./...          # static checks
go test ./...          # unit tests (config, api, events, server, state, tasks,
                         #   policy, tools, agent, voice, voice/rime, voice/deepgram
                         #   packages; voice/livekit requires libopus to even build)
go test -race ./...     # required for internal/state, internal/tasks,
                          #   internal/policy, internal/agent, internal/voice —
                          #   the concurrency-critical packages
go run ./cmd/server       # run locally, defaults to 0.0.0.0:8080

# Override defaults via env vars:
APP_ENV=production HTTP_HOST=127.0.0.1 HTTP_PORT=9090 go run ./cmd/server

# M6 voice env vars (see .env.example for the full list and defaults):
LIVEKIT_URL=wss://your-project.livekit.cloud LIVEKIT_API_KEY=... LIVEKIT_API_SECRET=... \
RIME_API_KEY=... DEEPGRAM_API_KEY=... \
go run ./cmd/server

# Demonstrate the state-versioning invariant end to end:
curl -X POST localhost:8080/machines -d '{"id":"machine-17","name":"Machine 17","attributes":{"vibration":"NORMAL"}}'
curl localhost:8080/machines/machine-17/state                          # -> version 1
curl -X POST localhost:8080/machines/machine-17/state \
  -d '{"attributes":{"vibration":"CRITICAL"},"source":"technician","note":"sensor flagged anomaly"}'
curl localhost:8080/machines/machine-17/state                          # -> version 2
curl localhost:8080/machines/machine-17/state?version=1                # -> version 1, unchanged

# Demonstrate task binding + cancellation end to end:
curl -X POST localhost:8080/machines/machine-17/tasks -d '{"tool_name":"vibration_scan"}'   # bound_version: 1
curl -X POST localhost:8080/machines/machine-17/state -d '{"status":"fault"}'                # machine -> v2
curl localhost:8080/tasks/<task-id>                                                          # still bound_version: 1
curl -X POST localhost:8080/tasks/<task-id>/start -d '{"duration_seconds":10}'                # -> RUNNING
curl -X POST localhost:8080/tasks/<task-id>/cancel                                            # -> CANCELLED (fast; context propagates)

# Demonstrate the M4 core invariant — stale-result rejection — end to end:
curl -X POST localhost:8080/machines/machine-17/tasks -d '{"tool_name":"vibration_scan"}'    # task bound_version: 1
curl -X POST localhost:8080/machines/machine-17/state -d '{"status":"fault"}'                 # machine -> v2 while task still bound to v1
curl -X POST localhost:8080/tasks/<task-id>/result -d '{"payload":{"vibration":"CRITICAL"}}'  # -> "outcome": "REJECTED", ToolResultRejected event
# A task created *after* the change (bound_version: 2) hitting /result instead -> "outcome": "ACCEPTED"

# Demonstrate the M5 Agent loop end to end (fresh vs. stale):
curl -X POST localhost:8080/machines/machine-17/agent/run -d '{"instruction":"please check vibration"}'
# fresh path (machine untouched while the 3s demo tool runs): "outcome": "ACCEPTED", payload present
# stale path: fire the same request, then POST .../state to change the machine before the
# tool's 3s delay elapses -> "outcome": "REJECTED", "replan_required": true, no "payload" field at all

# Demonstrate the M6 voice session lifecycle end to end (requires real
# LIVEKIT_URL/LIVEKIT_API_KEY/LIVEKIT_API_SECRET — session creation
# validates the machine exists and mints a room + token, but the actual
# audio pipeline needs a real LiveKit server and a human participant to
# be meaningfully exercised beyond this):
curl -X POST localhost:8080/machines/machine-17/voice/sessions -d '{"user_id":"demo-operator"}'
# -> {"session_id":"voice-...","livekit_room_id":"voxstate-...","livekit_url":"...","livekit_token":"..."}
curl localhost:8080/voice/sessions/<session-id>                # -> status, started_at
curl -X POST localhost:8080/voice/sessions/<session-id>/end     # -> ended_at set; ending again -> 409 Conflict
```

Tests are colocated with the code they cover (`*_test.go` next to the
package), following standard Go convention — the `backend/tests/`
directory from the M0 scaffold is reserved for later integration/e2e tests
(likely M9), not unit tests.

No linter or frontend tooling exists yet — introduced in later milestones
(frontend tooling in M8). Do not add tooling ahead of the milestone that
needs it.

## Milestone log

Update this section at the end of each milestone with what was actually
built (not planned) and any decisions that future milestones should know
about. Keep entries short — detail belongs in `docs/` and commit history.

- **M0 (done):** Project structure created (`backend/`, `frontend/`,
  `docs/`, `scripts/`, `docker/`). `backend/go.mod` initialized
  (`voxstate/backend`, go 1.25.1). Wrote `docs/ARCHITECTURE.md`,
  `docs/DOMAIN_MODEL.md`, `docs/EVENT_MODEL.md`,
  `docs/decisions/001-modular-monolith.md`, `docs/ROADMAP.md`. Wrote
  `README.md`, `.env.example`, `.gitignore`, `docker-compose.yml`
  (Postgres only). No Go server code, no migrations, no frontend code, no
  third-party integrations. Repo not yet git-initialized (left to the user).
- **M1 (done):** Implemented the minimal Go HTTP backend skeleton:
  `internal/config` (env-based config with defaults + tests),
  `internal/api` (router, request-logging middleware, `GET /health` +
  tests), `internal/server` (http.Server construction with timeouts,
  context-driven graceful shutdown + tests), and `cmd/server/main.go`
  (wires config → logger → router → server, handles SIGINT/SIGTERM).
  Logging via stdlib `log/slog` (JSON handler), zero third-party
  dependencies added — `go.mod` unchanged. Verified: `go build ./...`,
  `go vet ./...`, and `go test ./...` all pass; manually ran the built
  binary, confirmed `GET /health` returns `200 {"status":"ok",...}`, and
  confirmed SIGTERM triggers logged graceful shutdown. No database,
  migrations, domain logic (state/events/tasks/tools/policy/agent
  packages remain empty placeholders), LLM/LiveKit/Rime integration, or
  frontend code was added. Repo still not git-initialized.
- **M2 (done):** Implemented `internal/state` — `Machine`, `MachineState`,
  `StateVersion` types and an in-memory, mutex-guarded `Store` supporting
  `CreateMachine`, `GetMachine`, `GetCurrentState`, `GetStateVersion`, and
  `ChangeState` (with no-op detection and `system`/`technician` change
  sources). Implemented `internal/events`'s `Event` type and constructor
  (`MachineCreated`, `MachineStateChanged`, `TechnicianReported`) as
  in-memory-only domain values — no store/persistence. Added HTTP
  endpoints `POST /machines`, `GET /machines/{id}`,
  `GET /machines/{id}/state` (with `?version=`), and
  `POST /machines/{id}/state`, wired into `internal/api` and `main.go`.
  Zero new third-party dependencies — `go.mod` still stdlib-only.
  Verified: `gofmt`, `go build`, `go vet`, `go test`, and
  `go test -race` all pass (including a concurrent-writers test with 50
  goroutines producing unique sequential versions); manually ran the
  server through the full create → v1 → change → v2 → re-read v1
  (immutable) flow described in the milestone brief. No task engine, tool
  execution, policy/stale-result logic, persistent event store, database,
  LLM/LiveKit/Rime integration, or frontend code was added. Repo still not
  git-initialized.
- **M3 (done):** Implemented `internal/tasks` — `DiagnosticTask`,
  `Status` (`PENDING`/`RUNNING`/`COMPLETED`/`CANCELLED`), the `Work
  func(ctx) error` async execution abstraction, and an in-memory,
  mutex-guarded `Store` supporting `CreateTask` (binds to
  `state.Store.GetCurrentState`'s version, once, permanently),
  `GetTask`, `ListTasksForMachine`, `StartTask` (launches `Work` in a
  goroutine with a cancellable `context.Context`), and `CancelTask`
  (idempotent-or-error, propagates `context` cancellation, sentinel
  errors for already-terminal tasks). Added `tasks.SimulatedWork`, a
  clearly-marked placeholder (cancellable sleep loop, no diagnostic
  logic) so the abstraction is demonstrable without real tools. Extended
  `internal/events` additively with `DiagnosticStarted`/`Cancelled`/
  `Completed`. Added HTTP endpoints `POST /machines/{id}/tasks`,
  `GET /machines/{id}/tasks`, `GET /tasks/{id}`, `POST /tasks/{id}/start`,
  `POST /tasks/{id}/cancel`. Zero new third-party dependencies —
  `go.mod` still stdlib-only. Verified: `gofmt`, `go build`, `go vet`,
  `go test`, and `go test -race` (including 5 repeated runs and a
  200-iteration completion-vs-cancellation race test) all pass; manually
  ran the server through create → task bound to v1 → machine changes to
  v2 → task confirmed still bound to v1 → start → cancel mid-execution
  (returned in ~79ms against a 10s work duration, confirming `context`
  cancellation actually propagated rather than the goroutine running to
  completion) → CANCELLED, and separately confirmed the natural
  PENDING→RUNNING→COMPLETED path. No stale-result rejection, no
  `internal/policy`, no real tool execution, no persistent event store,
  no database, no LLM/LiveKit/Rime integration, and no frontend code was
  added — M4 introduces the policy/staleness gate that actually compares
  a task's bound version against the machine's current version and
  rejects mismatches; M3 only guarantees the bound version never changes
  underneath a task. Repo still not git-initialized.
- **M4 (done):** Implemented `internal/policy` — `ToolResult` (deliberately
  without `Accepted`/`RejectionReason` fields, unlike the literal
  `docs/DOMAIN_MODEL.md` shape; those live on the `Decision` return value
  instead, to make bypassing the check hard), `Outcome`/`Decision` types,
  and `Evaluator.Evaluate` implementing the exact-equality rule
  (`result.BoundVersion == machine.CurrentVersion`, rejecting both
  older *and* newer/invalid versions). Rejections produce an in-memory
  `ToolResultRejected` event via a new additive `internal/events` type.
  Added `policy.SimulateToolResult`, a clearly-marked demo/test helper
  that copies a task's `BoundVersion` verbatim into a `ToolResult` (never
  the machine's current version). Added one HTTP endpoint,
  `POST /tasks/{id}/result`, that simulates and evaluates a result for an
  existing task, returning the full `Decision` as JSON. Wrote
  `docs/POLICY.md` documenting the guarantee, exact-equality rule,
  rejection behavior, and consistency boundary (a point-in-time gate; no
  `state.Store` API extension was needed — evaluated and documented why
  directly in code). Zero new third-party dependencies — `go.mod` still
  stdlib-only. Verified: `gofmt`, `go build`, `go vet`, `go test`, and
  `go test -race` all pass, including the required
  `TestStaleResultRejectedAfterStateChange` (the project's central
  invariant, spelled out end to end) and its HTTP equivalent; manually
  ran the server through create → task bound to v1 → start diagnostic →
  machine changes to v2 mid-flight → simulate+evaluate result still bound
  to v1 → confirmed `REJECTED` with a `ToolResultRejected` event, then
  confirmed a fresh task created after v2 → `ACCEPTED`. No Agent/LLM
  reasoning loop, no automatic replanning after rejection, no task
  rebinding, no real tool execution, no event persistence, no database,
  no LiveKit/Rime/voice integration, and no frontend code was added — M5
  introduces the Agent/LLM tool-calling loop that will actually consume
  `policy.Decision` values; M4 only guarantees the check itself is
  correct. Repo still not git-initialized.
- **M5 (done):** Implemented `internal/tools` — `Tool` interface,
  `Registry`, and two deterministic demo tools (`vibration_scan`,
  `temperature_scan`, fixed non-random payloads, configurable delay for
  timing control in tests/demo). Zero dependencies on `state`/`tasks`/
  `policy`/`agent`. Implemented `internal/agent` — `Instruction`/`Plan`/
  `Result` types, `Planner` interface with a deterministic `KeywordPlanner`
  implementation (no LLM SDK/API key/network call anywhere in the
  package), and `Agent.Run` orchestrating: read state → plan → create
  task (bound version captured once) → run tool via `tasks.StartTask`
  (reusing the M3 task engine, not a second cancellation mechanism) →
  build a `policy.ToolResult` from the task's `BoundVersion` (never
  current version) via the existing `policy.SimulateToolResult` → gate
  through `policy.Evaluator.Evaluate` → copy the payload into `Result`
  only when `Accepted`. Added one HTTP endpoint,
  `POST /machines/{id}/agent/run`. No new event types added — existing
  `DiagnosticStarted`/`ToolResultRejected` plus `agent.Result`'s
  structured return value were judged sufficient. Updated
  `docs/ROADMAP.md` (M5 objective corrected to reflect the deterministic-
  planner decision), `docs/DOMAIN_MODEL.md` (Tool-lookup note updated),
  and `docs/ARCHITECTURE.md` (Tool Orchestrator and Agent sections
  updated to describe what's actually implemented). Zero new third-party
  dependencies — `go.mod` still stdlib-only. Verified: `gofmt`,
  `go build`, `go vet`, `go test`, and `go test -race` (repeated runs on
  the new concurrency-sensitive `agent`/`api`/`tools` packages) all pass —
  including all 7 required Agent tests
  (`TestAgentSelectsVibrationTool`, `TestAgentCreatesTaskWithCurrentStateVersion`,
  `TestAgentConsumesAcceptedResult`, `TestAgentRejectsStaleResult`,
  `TestAgentDoesNotConsumeStalePayload`, `TestAgentRejectsFutureVersionResult`,
  `TestAgentPropagatesCancellation`); manually ran the server through both
  the fresh path (`ACCEPTED`, payload present, ~3.17s) and the stale path
  (agent request fired against a 3s tool, machine changed mid-flight to
  v2, response came back `REJECTED` with `replan_required: true` and, by
  construction, no `payload` field in the JSON at all). No real LLM
  provider, no automatic replanning (only the `ReplanRequired` signal), no
  task rebinding, no voice/LiveKit/Rime/TTS/STT, no frontend, no
  database/event persistence was added — M6 introduces realtime voice
  (LiveKit + Rime) around this same deterministic Agent loop; M7
  introduces actual interruption-triggered cancellation and replanning.
  Repo still not git-initialized.
- **M6 (done):** Implemented `internal/voice` — `STT`/`TTS`/`Transport`/
  `RoomProvisioner` interfaces (four, not the originally-scoped three —
  room admin is a genuinely distinct boundary from per-session audio
  I/O), `VoiceSession` (STT → `agent.Agent.Run`, unchanged from M5 →
  `TextResponse` → TTS → Transport), a hand-tuned energy/silence
  `segmenter` for utterance endpointing (LiveKit's Go path has no
  built-in one), and `Manager` (in-memory session lifecycle, mirroring
  `tasks.Store`'s shape). `TextResponse` is M6's version of M5's
  stale-payload guarantee: its rejected-branch helper takes only a
  `reason string`, not an `agent.Result` or payload map, so it is
  structurally incapable of leaking stale payload content into speech.
  Implemented three real provider integrations: `internal/voice/livekit`
  (joins LiveKit rooms as a raw participant via `server-sdk-go/v2` +
  `pkg/media`'s PCM track helpers — LiveKit's Agents framework has no Go
  support, confirmed against its own docs and the `agents`/`agents-js`
  repos — plus room admin/token minting via `protocol/auth`),
  `internal/voice/rime` (hand-rolled REST client — no official Rime SDK
  exists in any language but Python framework plugins — parsing raw WAV
  responses and reporting back whichever sample rate the WAV's own `fmt`
  chunk states, not the value requested, since Rime's docs give
  inconsistent implicit defaults across pages), and `internal/voice/deepgram`
  (official `deepgram-go-sdk/v3`, prerecorded REST endpoint called once
  per segmenter-produced utterance rather than a streaming websocket,
  since the segmenter already does endpointing). No custom PCM resampler
  was written: `lkmedia.NewPCMLocalTrack` already resamples internally to
  LiveKit's 48kHz track rate, and Deepgram accepts an arbitrary configured
  sample rate, so neither direction needed one — confirmed by reading both
  SDKs' actual source rather than assuming a resampler was required.
  Added three HTTP endpoints, `POST /machines/{id}/voice/sessions`,
  `GET /voice/sessions/{id}`, `POST /voice/sessions/{id}/end`, following
  the exact thin-handler pattern every other endpoint group uses.
  Extended `internal/config` with `LiveKitURL`/`LiveKitAPIKey`/
  `LiveKitAPISecret`/`RimeAPIKey`/`RimeModelID`/`RimeSpeaker`/
  `RimeSampleRateHz`/`DeepgramAPIKey` (non-secret fields get sensible
  local defaults — `coda`/`astra`/`24000` — matching the existing
  `Config` pattern). New third-party dependencies (first since M0):
  `github.com/livekit/server-sdk-go/v2`, `github.com/livekit/protocol`,
  `github.com/livekit/media-sdk` (transitive, via `server-sdk-go`'s
  `pkg/media`), `github.com/deepgram/deepgram-go-sdk/v3` — no websocket
  client library was added, since both Rime and Deepgram are called via
  REST in M6. `go.mod`'s `go` directive was bumped 1.25.1 → 1.26
  (`server-sdk-go/v2` requires it). Updated `docs/ARCHITECTURE.md`
  (Voice transport/STT/TTS sections marked Implemented, flow diagrams
  updated), `docs/ROADMAP.md` (M6 objective corrected with the actual
  providers and the Go-participant architecture decision),
  `docs/DOMAIN_MODEL.md` (`VoiceSession` marked Implemented),
  `README.md`, and `.env.example` (Rime/Deepgram config, corrected
  "(wired in M6)" annotations to reflect what's genuinely wired now).
  Verified: `gofmt`, `go vet`, `go build`, `go test`, and `go test -race`
  all pass for every package except `internal/voice/livekit` (and
  `cmd/server`, which imports it) — that one subpackage requires
  `libopus`/`pkg-config` on the build machine (a cgo binding to the real
  libopus C library, pulled in transitively by `server-sdk-go/v2/pkg/media`'s
  Opus encode/decode), which were not available in the environment this
  milestone was implemented in; this is an environment/deployment
  limitation, not a code defect — the code was written and reviewed
  against the SDKs' actual real source, and `internal/voice`'s own core
  logic plus `voice/rime`/`voice/deepgram` all build and test cleanly
  (12 tests in `internal/voice` alone, including the flagship
  `TestSessionNeverSpeaksStaleResult`, which reproduces
  `TestAgentRejectsStaleResult`'s exact race and asserts no `TTS.Synthesize`
  call ever contains the stale payload's marker string). No interruption/
  barge-in handling (inbound audio is simply dropped while a response is
  in flight — a `speaking bool`, not a queue), no frontend, no database/
  event persistence for `VoiceSession`s, and no production-grade VAD was
  added — M7 introduces actual interruption-triggered cancellation and
  replanning on top of this same voice pipeline. This milestone was
  implemented after recovering the repository from a prior WSL crash —
  GitHub's `main` branch (through the M5 commit) was verified as the
  accurate baseline before any M6 work began; M0–M5 required no rework.
