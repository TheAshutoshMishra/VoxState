# VoxState Policy Layer

Status: **implemented as of M4** (`backend/internal/policy`). This
document describes what the policy layer actually guarantees today, not
aspirationally — see `docs/ROADMAP.md` for what remains for later
milestones.

This is the core correctness component of VoxState. Everything else in
the project (voice, LLM reasoning, tool orchestration) exists to produce
inputs to, or consume outputs from, this one check.

## What the policy guarantees

Before a `ToolResult` is allowed to influence anything downstream (Agent
reasoning, a spoken response — neither exists yet as of M4), it must pass
through `policy.Evaluator.Evaluate`. Evaluate always returns an explicit
`Decision` (never a bare `ToolResult` and never `nil`), and a caller has
no way to obtain a "probably fine" result without going through it —
`ToolResult` itself carries no `Accepted` field a caller could read (or
worse, set) directly.

## The exact equality rule

```
result.BoundVersion == machine.CurrentVersion   -> ACCEPT
result.BoundVersion != machine.CurrentVersion   -> REJECT
```

This is intentionally **equality**, not "not older than". A result whose
`BoundVersion` is *ahead of* the machine's current version is just as
invalid as one that's behind — it can only mean the result's provenance is
wrong, since a task can never be bound to a version that doesn't exist yet
at task-creation time (see `internal/tasks`, M3).

| current version | result version | outcome |
|---|---|---|
| 10 | 10 | ACCEPT |
| 11 | 10 | REJECT (stale) |
| 11 | 12 | REJECT (invalid — ahead of current) |

## Stale-result behavior / what happens on rejection

1. `Evaluate` returns `Decision{Outcome: Rejected, Reason: "...", Event: <ToolResultRejected>}`.
2. The `ToolResultRejected` event (`internal/events`) carries the task ID,
   result ID, the result's bound version, the machine's current version,
   and the human-readable reason — enough to reconstruct exactly why a
   result was thrown away, after the fact.
3. Nothing is persisted (M4 does not implement event persistence — see
   "What M4 does not guarantee" below). The event is an in-memory value
   returned to the caller.
4. The rejected `ToolResult` is still present on `Decision.Result` (for
   observability/debugging), but `Decision.Outcome` and
   `Decision.IsAccepted()` are unambiguous — a caller checking either gets
   the correct answer.

## Consistency boundary

`Evaluate` reads a machine's current version through
`state.Store.GetCurrentState`, which is a single mutex-guarded read — it
cannot observe a torn or partial version, and the comparison against
`result.BoundVersion` happens entirely against the value that read already
captured. There is no window between "read" and "compare" within a single
`Evaluate` call where a concurrent state change could affect that call's
own outcome.

What `Evaluate` does **not** guarantee: that the machine is still at the
version it just checked by the time a caller *acts* on an `ACCEPTED`
`Decision`. The machine can move to a new version immediately after
`Evaluate` returns. This is a deliberate **point-in-time gate**, not a
distributed transaction — VoxState does not hold the state store locked
across "evaluate + consume". The mitigation is architectural: callers
(the Agent, once it exists in M5+) must call `Evaluate` as close as
possible to the point of actually consuming a result, not cache a
`Decision` and trust it indefinitely. No `state.Store` API extension was
needed to make the check itself atomic — see the doc comment on
`Evaluate` in `backend/internal/policy/evaluator.go` for the reasoning.

## What M4 intentionally does not guarantee yet

- **No automatic replanning.** Rejecting a result does not trigger the
  Agent to re-run a diagnostic or do anything else — there is no Agent
  yet (M5).
- **No task rebinding.** A task's `BoundVersion` is never updated by the
  policy layer or anything else — `internal/tasks` already guarantees
  this (M3); policy only reads it.
- **No enforcement beyond `Evaluate` itself.** The policy layer makes
  misuse *difficult* (no mutable `Accepted` field, no way to get a result
  without a `Decision`), but it cannot stop a caller from computing a
  `Decision` and then simply not checking `IsAccepted()` before using
  `Decision.Result` anyway. M4 provides the safety boundary; using it
  correctly is still the caller's responsibility until M5's Agent is the
  sole consumer.
- **No persistence.** `ToolResultRejected` events exist only as
  in-memory values for the duration of the call that produced them.
- **No real tools.** `policy.SimulateToolResult` copies a task's bound
  version into a `ToolResult` for demonstration/testing only — it
  performs no real diagnostic work. Real tool execution is a later
  milestone.
