# VoxState Architecture

Status: M0–M9 complete (final milestone). This document describes the target
architecture for the system as a whole. Components described here are **not
yet implemented** unless explicitly noted as Implemented — see `CLAUDE.md`'s
"Current status" and milestone log for exactly what that covers.

## 1. The problem this architecture serves

A realtime voice agent talks to a user about a physical system (a machine) whose
state can change independently of the conversation — a technician does work on
it, a sensor reports a fault, another operator issues a command. Meanwhile the
agent may have already started long-running work (diagnostic tools) against the
state as it understood it a moment ago.

Two failure modes must be prevented:

1. **Stale results leaking into speech.** A tool call started against an old
   world state finishes after the world has changed, and its result is spoken
   as if it were still true.
2. **Uncancelled work outliving its relevance.** The user interrupts
   ("stop, that's already fixed") but background tasks keep running, consuming
   resources and racing to produce output that must then be suppressed anyway.

Every architectural decision below exists to make these two failure modes
structurally hard to hit, not just handled by convention.

## 2. Component overview

```
                              ┌─────────────────────────┐
                              │        Frontend          │
                              │   (Next.js / React / TS) │
                              │  state timeline, tasks,  │
                              │  policy, voice, activity │
                              └────────────▲──────────────┘
                                           │ REST (read + demo controls;
                                           │ LiveKit media connects directly
                                           │ to the LiveKit room, not through
                                           │ this backend)
                                           │
┌───────────────────────────────────────────────────────────────────────┐
│                         Go Backend (modular monolith)                  │
│                                                                          │
│   ┌────────────┐   audio/text   ┌────────────────┐                     │
│   │  LiveKit    │◄──────────────►│   API / Session │                     │
│   │  (voice     │   room events  │   layer (api/)  │                     │
│   │  transport) │                └───────┬─────────┘                     │
│   └────────────┘                        │                                │
│                                          ▼                                │
│                                  ┌──────────────┐                         │
│                                  │  Agent (LLM)  │  reasons, decides       │
│                                  │  agent/       │  tool calls, replans    │
│                                  └──────┬───────┘                         │
│                                          │ issues / cancels                │
│                                          ▼                                │
│   ┌───────────────┐   bound to    ┌──────────────┐   reads/writes   ┌───────────────┐
│   │  Tool          │◄─────────────│  Task Engine  │◄────────────────►│  State Engine  │
│   │  Orchestrator  │  state ver.  │  tasks/       │   current ver.    │  state/        │
│   │  tools/        │              └──────┬───────┘                   └───────┬───────┘
│   └───────┬───────┘                       │                                   │
│           │ results (tagged w/ state ver) │                                   │
│           ▼                                ▼                                   ▼
│   ┌────────────────────────────────────────────────────────────────────────┐  │
│   │                  Policy layer (policy/) — validates staleness           │  │
│   │      "was this produced against the current state version?"            │  │
│   └───────────────────────┬────────────────────────────────────────────────┘  │
│                            │ accept / reject                                    │
│                            ▼                                                    │
│                     back to Agent for response synthesis                        │
│                                                                                   │
│   ┌──────────────────────────────────────────────────────────────────────┐     │
│   │                    Event Store (events/) — append-only                │◄────┘
│   │   every state change, task lifecycle step, rejection is an event      │
│   └──────────────────────────────────┬───────────────────────────────────┘
│                                       │ persisted
└───────────────────────────────────────┼─────────────────────────────────────┘
                                        ▼
                              ┌──────────────────┐
                              │   PostgreSQL       │
                              │  events, state,     │
                              │  tasks, results      │
                              └──────────────────┘

   User speech ──► LiveKit ──► Deepgram (STT) ──► transcript ──► Agent
   Agent's final response text ──► Rime (TTS) ──► LiveKit ──► user hears it
```

## 3. Components and responsibilities

### API / Session layer (`internal/api`)
Entry point for the frontend (REST — this is exactly what `frontend/lib/api.ts`
consumes, see M8) and, as of M6, for minting/ending voice sessions
(`POST /machines/{id}/voice/sessions`, `GET`/`POST /voice/sessions/{id}...`)
— delegates all session lifecycle to `internal/voice.Manager`. Contains no
business logic itself, matching every other handler group in the package.

### State Engine (`internal/state`)
Owns the current `MachineState` and its version number per machine. Every
accepted change produces a new immutable state version. This is the single
source of truth that all other components check against. It never rewrites
history — it appends.

### Event Store (`internal/events`)
Append-only log of everything that happened: state changes, task lifecycle
transitions, interruptions, rejections. This is what makes the system
observable and debuggable, and it is the mechanism the frontend uses to render
a timeline. State Engine changes and Task Engine transitions are themselves
recorded here, so the event log is the audit trail, not a derived cache.

### Task Engine (`internal/tasks`)
Creates and tracks `DiagnosticTask` records. Every task is stamped with the
state version that was current at the moment it was created. Exposes
cancellation (a task has a cancel signal that tool execution must observe).
Does not itself decide whether a task's result is usable — that is the
Policy layer's job.

### Tool Orchestrator (`internal/tools`)
Executes the actual diagnostic/tool work (currently deterministic
simulated tools for the demo — `vibration_scan`, `temperature_scan`).
**Implemented (M5)** as a small `Tool` interface plus a `Registry`. A tool
itself is deliberately not state/task/policy-aware — it receives a
minimal `RunInput` and a cancellation-aware `context.Context`, and
returns a raw payload. It is the caller (the Agent) that stamps a task's
`BoundVersion` onto the tool's output to form a `policy.ToolResult` —
tools never see or attach a state version themselves, which keeps them
trivially decoupled from the versioning machinery they must never be able
to bypass.

### Policy layer (`internal/policy`)
The gatekeeper. Before any tool result or task output is allowed to influence
what the agent says, Policy checks: *is the state version this result was
produced against still the current version?* If not, the result is rejected
and a `ToolResultRejected` event is recorded. This is the mechanical
enforcement of the stale-result rule (see §7).

### Agent (`internal/agent`)
Orchestrates the reasoning/tool-calling loop: reads current machine state,
asks a `Planner` which tool to run, creates a task bound to that state's
version, runs the tool, and gates the result through the Policy layer
before consuming it. **Implemented (M5)** with a deterministic
`KeywordPlanner` — no LLM provider is wired in yet. `Planner` is the
narrow interface a real LLM/reasoning provider will implement in a later
milestone; introducing it now would be premature given M5's scope. As of
M6, `internal/voice.Session` feeds this same `Agent.Run` the transcript
text Deepgram produced — the Agent itself is unchanged, unaware it's now
reachable from voice as well as HTTP. M7 adds interruption handling
entirely in `internal/voice`, one layer above the Agent: `Agent.Run`
itself is not modified — it already stops and cancels its task the
moment its `ctx` is cancelled (M5), so `voice.VoiceSession` cancelling a
turn's context on barge-in is enough to reach the Agent through the exact
same, unchanged path.

### Voice transport — LiveKit
**Implemented (M6)** in `internal/voice/livekit`. LiveKit's "Agents"
framework — the thing that would normally run STT/LLM/TTS orchestration
for you inside a room — is Python and Node.js/TypeScript only; there is
no Go support and none is planned (confirmed against LiveKit's own docs
and both the `livekit/agents` and `livekit/agents-js` repos). The Go
backend therefore joins each session's room directly as a raw participant
via `server-sdk-go/v2`, using its `pkg/media` helpers
(`lkmedia.PCMLocalTrack`/`PCMRemoteTrack`) for Opus encode/decode instead
of a framework that doesn't exist for Go. `internal/voice/livekit` also
mints human-participant access tokens (`protocol/auth.AccessToken`) and
handles room admin (create/delete) via `RoomServiceClient` — see
`voice.RoomProvisioner` and `voice.Transport`.

Room membership and interruption/VAD signals: LiveKit itself does not
hand the Go SDK a ready-made "user started/stopped talking" event the way
its Agents framework would for Python/Node — `internal/voice`'s own
`segmenter.go` does simple energy/silence-threshold utterance
segmentation instead (see the Tool Orchestrator/Agent-adjacent "Voice
session" flow below). This is a known, hand-tuned, non-production-grade
heuristic, not adaptive VAD. M7's barge-in detection reuses this same
heuristic (`segmenter.loud`) rather than a separate "is this an
interruption" signal — there is still only one VAD implementation in the
codebase. **Implemented (M7):** `voice.Transport` gained one new method,
`StopAudio`, alongside `Send`/`Frames`/`Close` — `livekit.Transport`
implements it as `lkmedia.PCMLocalTrack.ClearQueue()`, the same call the
SDK's own mute handling uses internally, because `Send`/`WriteSample`
only enqueue samples into a buffer a background goroutine paces out over
real time; cancelling a turn's context after `Send` has already returned
does nothing on its own to audio already sitting in that buffer.

### STT — Deepgram
**Implemented (M6)** in `internal/voice/deepgram`, using Deepgram's
official Go SDK (`deepgram-go-sdk/v3`). Not pinned down by any earlier
milestone's docs (unlike LiveKit/Rime, which M0's README already named) —
selected during M6 implementation. Calls Deepgram's prerecorded REST
endpoint once per utterance `segmenter.go` already produced, rather than
holding open a streaming websocket: since the Go LiveKit path forces
`internal/voice` to do its own endpointing anyway, a second, redundant
streaming endpointing layer underneath it would add complexity without a
latency benefit once an utterance is already a complete, bounded buffer.

### TTS — Rime
**Implemented (M6)** in `internal/voice/rime`. Converts the agent's
final, policy-validated response text to speech. Only ever receives text
that has passed the staleness check — see `voice.TextResponse`, the M6
equivalent of M5's "the Agent never consumes a stale payload" guarantee.
Rime has no official SDK in any language except Python framework plugins,
so this is a hand-rolled REST client (`POST /v1/rime-tts`, requesting WAV
output and parsing the returned RIFF container directly). Every request
states `samplingRate` explicitly — Rime's own documentation gives
inconsistent implicit defaults across different pages — and `Synthesize`
reports back whichever sample rate the response's own `fmt` chunk states,
not the value requested, so a mismatch between what was asked for and
what Rime actually produced can never silently propagate downstream.

### Frontend (Next.js/React/TS)
**Implemented (M8)** in `frontend/`. A control surface, not a read-only
viewer: it both displays machine state/versions/tasks/policy
decisions/voice sessions/activity and drives the backend through its
existing HTTP API (create/select a machine, change state, create/start/
cancel a task, run the agent, start/end a voice session) — a demo needs
to *cause* a version bump and a stale result, not just watch one that
already happened. One panel per concern: `MachinePanel` (current state +
version + change-state form), `StateTimeline` (every historical version,
oldest-first), `TaskPanel` (task list with a bound-version-vs-current
MATCH/STALE indicator), `PolicyPanel` (manual result evaluation, a "run
agent" control, and a one-click guided reproduction of the flagship
stale-result scenario), `VoicePanel` (real `livekit-client` session
connected via `POST /machines/{id}/voice/sessions`, with a derived
ACTIVE/INTERRUPTED/COMPLETED list per turn), and `ActivityStream` (a
merged, newest-first feed of backend events). Polling (`usePoll`, ~1s for
per-machine panels) keeps every panel current without adding a
websocket/SSE layer this milestone doesn't need.

**M8: backend authority.** The frontend must never become a second,
possibly-inconsistent implementation of the version-fencing check that
`policy.Evaluator` (M4) owns. Concretely: no frontend code compares a
task's bound version against a machine's current version to produce an
ACCEPTED/REJECTED verdict — `PolicyPanel` only ever renders
`decision.outcome` exactly as `POST /tasks/{id}/result` returned it, and
`agentResult.outcome` exactly as `POST /machines/{id}/agent/run`
returned it. `TaskPanel`'s bound-vs-current MATCH/STALE pill is the one
place the frontend does compare two version numbers itself, but it is a
passive display hint (the same two numbers the backend already returned
in that task's response), never a gate on what the UI does next. This
boundary is why `frontend/lib/api.ts` contains no business logic: every
function in it is a typed pass-through to one backend route, returning
exactly what the backend responded with.

### PostgreSQL
Durable storage for events, state versions, tasks, and tool results. Chosen
over an in-memory-only approach because the event log and state history are
core to demonstrating the problem (you need to be able to show, after the
fact, exactly what was rejected and why).

## 4. Data flow (steady state, no interruption)

```
user speaks → LiveKit → API layer → Agent
Agent decides a tool is needed → Task Engine creates DiagnosticTask
    bound to current StateVersion(N) → Tool Orchestrator runs tool
Tool finishes → result tagged StateVersion(N) → Policy layer checks:
    current version still N? → yes → Agent synthesizes response → Rime → LiveKit → user hears it
Every step above also emits an Event to the Event Store.
```

## 5. Voice flow

**Implemented (M6, interruption added in M7):**

```
Mic audio → LiveKit room → voice.Transport (raw participant, pkg/media)
    → voice.segmenter (energy/silence VAD) → complete utterance
    → Deepgram (voice.STT) → transcript text
    → agent.Agent.Run (unchanged from M5) → agent.Result
    → voice.TextResponse (Accepted → payload text; Rejected → safe
      fallback text, never the stale payload) → Rime (voice.TTS)
    → PCM audio → voice.Transport.Send → LiveKit room → user's speakers
```

Every instruction is a logical **turn** (M7): `VoiceSession.Run`'s frame
loop keeps consuming inbound audio while a turn is being processed or
spoken, instead of blocking on it, so it can detect the user talking over
an active turn. See §7 below for the interruption flow this enables.

## 6. State flow

```
Event occurs (technician report, sensor update, manual override, etc.)
    → State Engine validates it → new immutable MachineState created
    → StateVersion incremented (N → N+1)
    → StateChanged event recorded in Event Store
    → any task bound to version N is now operating against a stale version
```

State versions are per-machine and strictly increasing. Nothing mutates a
past version; the system only ever moves forward.

## 7. Task flow, interruption flow, and stale-result flow

This is the core of VoxState and is documented in detail in
`DOMAIN_MODEL.md` (entities) and inline below (behavior).

**Task flow**
```
Agent requests diagnostic → Task Engine stamps task with StateVersion(N)
    → Tool Orchestrator executes, watching a cancellation signal
    → on completion, result is tagged with StateVersion(N)
```

**Interruption flow** (**implemented, M7**)
```
User speaks over an active turn (loud inbound frame while
voice.VoiceSession has a current turn)
    → VoiceSession.interrupt: cancel that turn's context
        → propagates into agent.Agent.Run/tasks.Store exactly like any
          other ctx cancellation already did in M6 — the same single
          cancellation path, not a second mechanism
        → Task Engine flips the bound task's status to Cancelled (or, if
          the task had already completed, the CANCELLED transition is a
          documented no-op — see §7's stale-result flow: the Policy layer
          still rejects that result on version mismatch regardless)
    → Transport.StopAudio discards any outbound audio already queued for
      playback, since Transport.Send only enqueues for asynchronous,
      real-time-paced delivery and cancelling a context after Send
      returns does not by itself stop already-queued audio
    → UserInterrupted + ResponseInvalidated events recorded
    → the same inbound frame that triggered the interruption begins
      accumulating the next utterance, which becomes the new, active turn
    → Agent processes the new instruction normally (agent.Run is
      unmodified by M7); the interrupted turn's result, even if it
      eventually completes and is policy-accepted, is never spoken —
      voice.Session.handleUtterance checks the turn's own ctx.Err()
      immediately before every user-visible step (TTS synthesis,
      Transport.Send), not just once
```

This reuses M3's cancellation mechanism and M4's policy check unchanged —
voice does not duplicate `result.BoundVersion == machine.CurrentVersion`
anywhere; that comparison stays exclusively inside `policy.Evaluator`. The
turn-ownership check (`ctx.Err()`) is a separate, voice-layer-only concern:
whether a turn has been superseded by a newer one, not whether a state
version is stale.

**Stale-result flow (concrete walkthrough)**
```
State v10 exists.
Agent starts DiagnosticTask T1, bound to StateVersion v10.
While T1 runs, a technician report changes the machine → State v11 created.
T1 finishes and returns a result tagged v10.
Policy layer compares: task's bound version (v10) != current version (v11).
Result is rejected. ToolResultRejected event recorded.
Agent is informed the result is stale, not handed the payload.
Agent replans against v11 (e.g., re-runs diagnostics, or answers directly
    if v11 already contains the answer).
Rime speaks a response that only reflects v11.
```

This flow is what M4 (Stale Result Protection) will implement and what the
demo script is built around.

## 8. Why a modular monolith

See `docs/decisions/001-modular-monolith.md`. In short: at hackathon scope,
network-boundary microservices would add distributed-systems failure modes
(partial failure, network staleness) that are *not* the problem we're trying
to demonstrate — we want staleness to be a property of world state and task
binding, not an artifact of service-to-service lag. A single Go process with
clearly separated internal packages gives us clean boundaries without that
noise, and nothing here is deployment-target-locked — packages could be
split out later if ever needed.
