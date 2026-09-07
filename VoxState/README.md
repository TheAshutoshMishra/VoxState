# VoxState

> **Status: in development — Milestone 7 (Voice Interruption & Recovery) is
> complete.** M0–M7 are implemented; see `docs/ROADMAP.md` and
> `CLAUDE.md`'s "Current status" section for exactly what that does and
> does not cover. The frontend (M8) and a real LLM provider are not built
> yet.

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
5. The in-progress voice turn is invalidated: queued/in-flight response
   audio is stopped and the diagnostic task bound to the superseded turn
   is cancelled through the same task engine used everywhere else.
6. If the cancelled task's result arrives anyway, it's still rejected by
   the unchanged M4 policy check — interruption doesn't add a second,
   parallel staleness check.
7. The new instruction becomes the active turn; the agent replans against
   current state.
8. Rime speaks a response that reflects only the current, verified state
   — never anything from the interrupted turn.

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

**M7 — Voice Interruption & Recovery.** `internal/voice.VoiceSession` now
recognizes every instruction as a logical **turn**: Run's frame loop keeps
consuming inbound audio while a turn is being processed/spoken, and a
loud frame arriving while a turn is active is treated as a barge-in — the
active turn's context is cancelled (propagating into `agent.Run`/
`tasks.Store` exactly like any other cancellation already did in M6),
`Transport.StopAudio` discards any audio already queued for playback, and
the same frame starts accumulating the interrupting utterance as the new,
authoritative turn. See `docs/ARCHITECTURE.md`'s voice-flow section and
`CLAUDE.md`'s M7 design notes for the full turn lifecycle and the races
this closes. Talking over the agent now reliably cuts it off instead of
being silently dropped.

See `docs/ROADMAP.md` for the full milestone breakdown (M0–M9) and
`CLAUDE.md` for exact per-milestone status and design notes.

## How this will eventually be demonstrated

A live voice conversation with the agent about a simulated machine fleet:
the user asks for a diagnosis, the agent starts work, the demo operator
triggers a state change (via the frontend or a script) mid-diagnostic, and
the user interrupts. The frontend timeline (M8, not built yet) will show
the state version bump, the task cancellation, and the rejected stale
result in realtime, alongside the agent's spoken (Rime) response
reflecting only current state. As of M7, every piece of that flow except
the frontend visualization is demonstrable end to end: interrupting the
agent mid-response now cancels its in-flight work and cuts off its audio
immediately.

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
