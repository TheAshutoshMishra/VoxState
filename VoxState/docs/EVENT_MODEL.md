# VoxState Event Model

Status: M0 — Foundation. Describes the initial event catalog conceptually.
No event bus, dispatcher, or persistence is implemented yet (that's M2).

## Why events are central

VoxState's core claim is that stale information must never silently
influence a spoken response. The only way to make that claim checkable is if
every state change, task transition, and rejection is recorded as a discrete,
timestamped fact. The event log is that record — it's simultaneously the
audit trail, the debugging tool, and the frontend's data source for the
timeline visualization used in the demo.

Events are append-only. Nothing in VoxState mutates or deletes an event.

## Common shape

Every event has:

- `id`
- `type` (one of the catalog below)
- `machine_id` (nullable)
- `session_id` (nullable)
- `payload` (JSON, shape specific to `type`)
- `resulting_state_version` (nullable — present when the event is what
  caused a machine to move to a new `StateVersion`)
- `created_at`

## Event catalog

### MachineCreated
A machine was registered in the system. Establishes `StateVersion(1)` for
that machine. In the demo, this happens at seed/setup time, not live.

### MachineStateChanged
The world changed: a sensor reading, a manual status update, or a technician
action altered what's true about the machine. This is the event that
produces a new `StateVersion`. Every `MachineStateChanged` event has a
`resulting_state_version` set.

### TechnicianReported
A human reported information about the machine (e.g. "motor replaced").
Distinguished from `MachineStateChanged` because it's a *source* of a state
change with different trust/authority characteristics (human report vs.
sensor data) — it typically triggers a `MachineStateChanged` as a
consequence, but is recorded separately so the provenance of *why* the state
changed is preserved.

### DiagnosticStarted
A `DiagnosticTask` was created and bound to the current `StateVersion`.
Marks the beginning of a task's lifecycle in the log.

### DiagnosticCancelled
A `DiagnosticTask` was cancelled before completion — typically as a direct
consequence of `UserInterrupted`, but could also happen if the bound state
version is superseded before the tool even finishes (proactive cancellation,
rather than waiting to reject the result after the fact).

### DiagnosticCompleted
A `DiagnosticTask` finished and produced a `ToolResult`. This event fires
regardless of whether the result is later accepted or rejected — completion
and validity are separate concerns.

### UserInterrupted
The user spoke over the agent (LiveKit interruption signal) or explicitly
told the agent to stop/change course. This is the trigger event that leads
the Agent to request cancellation of in-flight tasks.

### ToolResultRejected
The Policy layer determined that a `ToolResult`'s `produced_for_state_version`
does not match the machine's current `StateVersion`, and refused to let it
influence the response. This is the mechanical enforcement point for the
stale-result rule — the single most important event type in the system for
demonstrating correctness.

### StateConflictDetected
Two sources of truth disagree about the current state (e.g. a technician
report and a sensor reading arrive close together with contradictory
information). Distinct from a normal `MachineStateChanged` because it flags
that the Agent/Policy layer needs to reconcile or surface the conflict
rather than silently taking the latest write as truth.

### ResponseInvalidated
A response the Agent was in the process of formulating (or had already
started speaking) was invalidated by a state change or interruption before
it reached the user, or was invalidated by a `ToolResultRejected`. Used to
show, in the frontend timeline, the moments where the system caught itself
before saying something wrong.

## What's intentionally deferred

- No formal event schema versioning yet (single JSON payload shape per
  type, no migration strategy for payload shape changes). Add if M2 shows
  a real need.
- No pub/sub or fan-out mechanism specified yet — whether internal
  components consume events via direct function calls, an in-process
  channel, or a listen/notify pattern on Postgres is an M2 implementation
  decision, not an M0 concern.
