# VoxState Domain Model

Status: M0 — Foundation. Conceptual model only. No schema migrations or Go
structs are implemented yet — this defines the vocabulary M1+ will build on.

## Entities

### Machine
The physical thing being monitored/serviced.

- `id`
- `name` (e.g. "Machine 17")
- `type` (e.g. "CNC mill", "conveyor")
- `location`
- `current_state_version` (pointer to the latest `StateVersion`)

A `Machine` is long-lived. It does not itself carry current status — that
lives in `MachineState`, because status is exactly the thing that changes and
needs versioning.

### StateVersion
A monotonically increasing integer per machine, paired with the
`MachineState` snapshot it identifies. This is the mechanism the whole
stale-result system hinges on.

- `machine_id`
- `version` (integer, strictly increasing per machine, starts at 1)
- `created_at`
- `caused_by_event_id` (the `Event` that produced this version)

### MachineState
An immutable snapshot of what is true about a machine at a given
`StateVersion`.

- `machine_id`
- `state_version` (FK to `StateVersion.version`)
- `status` (e.g. `running`, `fault`, `under_maintenance`, `stopped`)
- `attributes` (free-form key/value — sensor readings, notes, whatever the
  demo scenario needs; kept loose deliberately, see "Not over-engineering"
  below)
- `created_at`

Once written, a `MachineState` row is never updated. A change in the world
produces a *new* `MachineState` at a *new* `StateVersion`, never an edit to
an existing one.

### Event
The append-only log entry. Every state change, task lifecycle transition,
interruption, and rejection is recorded as an `Event`. This is the audit
trail and the data source for the frontend timeline.

- `id`
- `type` (see `EVENT_MODEL.md` for the catalog)
- `machine_id` (nullable — some events are session-scoped, not machine-scoped)
- `session_id` (nullable — some events are machine-scoped, not session-scoped)
- `payload` (JSON, shape depends on `type`)
- `resulting_state_version` (nullable — set when the event caused a new
  `StateVersion`)
- `created_at`

### DiagnosticTask
A unit of work the Agent asked the Tool Orchestrator to perform, always
bound to the state version that was current when it was created.

- `id`
- `machine_id`
- `session_id` (the `VoiceSession` that requested it)
- `bound_state_version` (the `StateVersion.version` this task is valid
  against — set once, at creation, never changed)
- `tool_name`
- `status` (`pending`, `running`, `cancelled`, `completed`, `rejected`)
- `started_at`, `finished_at`

### ToolResult
The output of a `DiagnosticTask`, tagged with the version it was produced
against so the Policy layer can check it later.

- `id`
- `task_id` (FK to `DiagnosticTask`)
- `produced_for_state_version` (copied from the task's `bound_state_version`
  at production time — kept as its own field rather than only trusting a
  join, so a rejected result's provenance is self-contained)
- `payload`
- `accepted` (bool — set by the Policy layer)
- `rejection_reason` (nullable)
- `created_at`

### VoiceSession
One realtime conversation between a user and the agent. **Implemented
(M6)** as `internal/voice.VoiceSession` / `voice.SessionInfo`, held
in-memory by `voice.Manager` (no persistence, same as every other engine
today) — `DiagnosticTask` still has no persisted `session_id` FK; a
session's `MachineID` plays the role `focus_machine_id` describes here,
but the association is only ever in-memory, not written onto tasks
themselves.

- `id`
- `livekit_room_id`
- `user_id` (or anonymous identifier for the demo)
- `focus_machine_id` (nullable — which machine the conversation is currently
  about, so "diagnose it" resolves to the right machine)
- `started_at`, `ended_at`

## Relationships

```
Machine (1) ───< (N) StateVersion ───(1:1)─── MachineState
Machine (1) ───< (N) Event
Machine (1) ───< (N) DiagnosticTask
DiagnosticTask (1) ───< (N) ToolResult        (normally 1, but modeled as N
                                                to allow retries later)
DiagnosticTask (N) ───> (1) StateVersion       (bound_state_version)
VoiceSession (1) ───< (N) DiagnosticTask
VoiceSession (1) ───< (N) Event
Event (0..1) ───> (1) StateVersion             (resulting_state_version, when
                                                 the event caused a version bump)
```

## Deliberately not modeled yet (over-engineering guard)

- **Incident**: a container grouping multiple sessions/machines into one
  maintenance incident. Not needed until the demo requires multi-session
  correlation. Add only if M6+ shows a real need.
- **User/Technician as a full entity**: for the demo, a session just carries
  an identifier. A real technician-identity model is out of scope.
- **Tool as a persisted entity**: `internal/tools` (M5) looks tools up by
  name via an in-memory `Registry` — dynamic lookup exists now — but there
  is still no formal `Tool` entity/table with persisted capability
  metadata. That remains deferred until a milestone needs tools to be
  discoverable/configurable beyond a fixed, code-registered set.
- **Structured `attributes`/`payload` schemas**: kept as JSON blobs
  deliberately. Locking down per-machine-type attribute schemas now would be
  premature — the demo has one or two machine types.

## Why StateVersion is its own entity, not just a field on Machine

`Machine.current_state_version` is a pointer for convenience, but the
authoritative history lives in the `StateVersion`/`MachineState` pair as
independent, immutable rows. This is what lets a `DiagnosticTask` bind to
"version 10" and have that binding remain meaningful forever, even after the
machine has moved on to version 11, 12, 13. Without a durable, addressable
version history, there would be nothing concrete for the Policy layer to
compare against.
