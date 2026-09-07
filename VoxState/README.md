# VoxState

> **Status: in development — Milestone 6 (Realtime Voice Integration) is
> complete.** M0–M6 are implemented; see `docs/ROADMAP.md` and
> `CLAUDE.md`'s "Current status" section for exactly what that does and
> does not cover. Interruption handling (M7), the frontend (M8), and a
> real LLM provider are not built yet.

A realtime voice agent that stays consistent with the latest verified world
state — even when the user interrupts, external events change that state
mid-task, or a previously started tool returns a result that's no longer
true.

## The problem

Voice agents that call tools or run background work have a timing hole: the
world can change while a task is in flight. If the agent speaks whatever the
task eventually returns, it can confidently state something that stopped
being true seconds ago. Interruptions make this worse — the user says "stop,
that's already handled," but nothing guarantees the in-flight work actually
stops, or that its eventual output is kept out of the response.

## Proposed solution

Every meaningful change to the world creates a new, immutable **state
version**. Every task the agent starts is bound to the state version that
was current when it started. Before any tool result is allowed to influence
what the agent says, a policy check confirms the result was produced against
the *current* state version — not a stale one. Interruptions trigger
cancellation of in-flight tasks bound to the superseded version, and the
agent replans against current state before responding.

## Core technical innovation

Versioned world state + result provenance + a mandatory staleness check
before speech synthesis. Nothing is spoken that wasn't validated against the
current state version at the moment of speaking. This is enforced
mechanically (a policy layer that compares version numbers), not by
convention or prompt instruction.

## Demo environment

Industrial machine maintenance. Example flow:

1. User: "Diagnose Machine 17."
2. Agent starts diagnostic tools against the machine's current state.
3. Machine state changes mid-diagnostic (e.g. a technician report arrives).
4. User interrupts: "Stop. Maintenance replaced the motor."
5. In-flight diagnostics are cancelled; the outdated result is rejected if
   it arrives anyway.
6. Agent replans against the current state.
7. Rime speaks a response that reflects only the current, verified state.

## High-level architecture

```
LiveKit (voice) ⇄ Go backend (modular monolith) ⇄ PostgreSQL (state/events)
                        │
                        ├─ Agent (LLM reasoning + tool calls)
                        ├─ State Engine (versioned machine state)
                        ├─ Task Engine (state-bound tasks, cancellation)
                        ├─ Tool Orchestrator (diagnostic tools)
                        ├─ Policy layer (stale-result rejection)
                        └─ Event Store (append-only history)
                        │
                Next.js/React frontend (read-only observability:
                state timeline, task status, rejected results)
```

Full detail: `docs/ARCHITECTURE.md`. Entity definitions: `docs/DOMAIN_MODEL.md`.
Event catalog: `docs/EVENT_MODEL.md`.

## Planned stack

- **Backend:** Go (modular monolith — see `docs/decisions/001-modular-monolith.md`)
- **Persistence:** PostgreSQL (Redis only if a later milestone proves a
  concrete need) — not added yet; still in-memory
- **Frontend:** Next.js + React + TypeScript — not built yet (M8)
- **Realtime voice transport:** LiveKit — implemented (M6); the Go
  backend joins rooms as a raw participant, since LiveKit's Agents
  framework has no Go support
- **STT:** Deepgram — implemented (M6)
- **TTS:** Rime — implemented (M6)
- **Reasoning/tool-calling:** a deterministic `KeywordPlanner` — no real
  LLM provider wired in yet
- **Local dev/deploy:** Docker

## Current milestone

**M6 — Realtime Voice Integration.** `internal/voice` glues LiveKit
(transport), Deepgram (STT), and Rime (TTS) onto the M5 `agent.Agent`
loop unchanged: speech in → transcript → `Agent.Run` → policy-gated
result → response text → speech out. A rejected (stale) result's payload
still never reaches speech — see `voice.TextResponse`. Interruption
handling (talking over an in-progress response) is not implemented yet —
inbound audio is simply dropped while the agent is speaking; that's M7.

See `docs/ROADMAP.md` for the full milestone breakdown (M0–M9) and
`CLAUDE.md` for exact per-milestone status and design notes.

## How this will eventually be demonstrated

A live voice conversation with the agent about a simulated machine fleet:
the user asks for a diagnosis, the agent starts work, the demo operator
triggers a state change (via the frontend or a script) mid-diagnostic, and
the user interrupts. The frontend timeline will show the state version bump,
the task cancellation, and the rejected stale result in realtime, alongside
the agent's spoken (Rime) response reflecting only current state.
Interruption reaction (the "user interrupts" step) is M7 — as of M6, the
voice conversation and the stale-result-never-spoken guarantee are both
demonstrable, but talking over the agent doesn't yet trigger cancellation.

## Project structure

```
voxstate/
├── backend/           # Go modular monolith
│   ├── cmd/           # entrypoints
│   ├── internal/
│   │   ├── api/       # HTTP entry points, incl. voice session lifecycle
│   │   ├── state/     # Machine / StateVersion / MachineState
│   │   ├── events/    # append-only event log
│   │   ├── tasks/     # DiagnosticTask lifecycle + cancellation
│   │   ├── tools/     # tool orchestration/execution
│   │   ├── agent/     # reasoning + tool-calling loop (deterministic planner)
│   │   ├── policy/    # stale-result rejection logic
│   │   └── voice/     # M6: LiveKit/Rime/Deepgram voice adapter around Agent
│   ├── migrations/
│   └── tests/
├── frontend/          # Next.js/React/TS observability UI
├── docs/              # architecture, domain model, event model, ADRs, roadmap
├── scripts/
├── docker/
└── docker-compose.yml
```
