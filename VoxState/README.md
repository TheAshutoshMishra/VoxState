# VoxState

> **Status: in development — Milestone 8 (Frontend & Visualization) is
> complete.** M0–M8 are implemented; see `docs/ROADMAP.md` and
> `CLAUDE.md`'s "Current status" section for exactly what that does and
> does not cover. A real LLM provider is not wired in yet (a deterministic
> keyword planner stands in for it — see `internal/agent`).

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
                Next.js/React frontend (control surface: machine/state/
                task/policy/voice panels, demo controls, activity stream)
```

Full detail: `docs/ARCHITECTURE.md`. Entity definitions: `docs/DOMAIN_MODEL.md`.
Event catalog: `docs/EVENT_MODEL.md`.

## Planned stack

- **Backend:** Go (modular monolith — see `docs/decisions/001-modular-monolith.md`)
- **Persistence:** PostgreSQL (Redis only if a later milestone proves a
  concrete need) — not added yet; still in-memory
- **Frontend:** Next.js + React + TypeScript — implemented (M8)
- **Realtime voice transport:** LiveKit — implemented (M6); the Go
  backend joins rooms as a raw participant, since LiveKit's Agents
  framework has no Go support
- **STT:** Deepgram — implemented (M6)
- **TTS:** Rime — implemented (M6)
- **Reasoning/tool-calling:** a deterministic `KeywordPlanner` — no real
  LLM provider wired in yet
- **Local dev/deploy:** Docker

## Current milestone

**M8 — Frontend & Visualization.** A Next.js/React/TypeScript dashboard
in `frontend/` makes every M2–M7 mechanic visible and operable: machine
state + version, a full state-version timeline, tasks with a bound-vs-
current-version indicator, a policy panel that renders the backend's own
ACCEPTED/REJECTED verdicts (including a one-click reproduction of the
stale-result scenario below), a real LiveKit-connected voice panel with
per-turn ACTIVE/INTERRUPTED/COMPLETED status, and a merged activity
stream. The frontend contains no copy of the version-fencing check — see
`docs/ARCHITECTURE.md`'s "M8: backend authority" note. A small additive
backend piece, `internal/activity` (`GET /activity`), surfaces
background-goroutine events (task lifecycle, M7 voice turns) that have
no HTTP request to ride along on. See `docs/ROADMAP.md` and `CLAUDE.md`'s
M8 entry for the full breakdown.

See `docs/ROADMAP.md` for the full milestone breakdown (M0–M9) and
`CLAUDE.md` for exact per-milestone status and design notes.

## How this is demonstrated

A live voice conversation with the agent about a simulated machine fleet:
the user asks for a diagnosis, the agent starts work, the demo operator
triggers a state change (via the frontend or a script) mid-diagnostic, and
the user interrupts. The frontend (`frontend/`) shows the state version
bump, the task cancellation, and the rejected stale result in realtime —
including a one-click guided replay of that exact scenario in
`PolicyPanel` — alongside the agent's spoken (Rime) response reflecting
only current state. Every piece of that flow is demonstrable end to end
as of M8: interrupting the agent mid-response cancels its in-flight work
and cuts off its audio immediately, and the frontend makes all of it
visible without reading logs.

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
├── frontend/          # Next.js/React/TS control surface (M8)
├── docs/              # architecture, domain model, event model, ADRs, roadmap
├── scripts/
├── docker/
└── docker-compose.yml
```

## Setup & running

```bash
# Backend — internal/config reads real OS environment variables only
# (no .env auto-loading exists, by design — see internal/config/config.go's
# doc comment). .env.example is a copy-able reference, not something the
# Go binary reads automatically.
cd backend
go run ./cmd/server             # sensible defaults for everything except
                                 #   LiveKit/Rime/Deepgram credentials (blank
                                 #   -> those endpoints error at call time);
                                 #   requires libopus/pkg-config (see Limitations)
# — or, without libopus/pkg-config or real voice credentials —
go run ./cmd/devserver          # identical HTTP API; LiveKit/Rime/Deepgram replaced
                                 #   with in-memory fakes (no real audio)

# To actually exercise the voice endpoints against real providers, export
# real values first (see .env.example for the full list), e.g.:
#   export LIVEKIT_URL=... LIVEKIT_API_KEY=... LIVEKIT_API_SECRET=... \
#          RIME_API_KEY=... DEEPGRAM_API_KEY=...
#   go run ./cmd/server

# Frontend (separate terminal) — Next.js DOES auto-load .env.local
cd frontend
npm install
cp .env.example .env.local      # NEXT_PUBLIC_API_URL, defaults to http://localhost:8080
npm run dev                     # http://localhost:3000
```

`docker compose up -d postgres` starts the only service currently defined
in `docker-compose.yml`; nothing in the codebase talks to it yet (see
Persistence, below) — it is not required to run either binary above.

## Environment variables

See `.env.example` (backend) and `frontend/.env.example` for the full,
current list with working local defaults. Summary:

| Variable | Required for | Notes |
|---|---|---|
| `HTTP_HOST`/`HTTP_PORT` | backend HTTP server | defaults `0.0.0.0`/`8080` |
| `APP_ENV` | backend | default `development` |
| `DATABASE_URL` | nothing yet | Postgres is provisioned by `docker-compose.yml` but unused by any Go code — see Limitations |
| `LIVEKIT_URL`/`LIVEKIT_API_KEY`/`LIVEKIT_API_SECRET` | real voice sessions (`cmd/server`) | blank works with `cmd/devserver` |
| `RIME_API_KEY` | real TTS | `RIME_MODEL_ID`/`RIME_SPEAKER`/`RIME_SAMPLE_RATE_HZ` have working defaults (`coda`/`astra`/`24000`) if unset — see `RIME_EVIDENCE.md` |
| `DEEPGRAM_API_KEY` | real STT | required for `cmd/server`'s voice path |
| `LLM_API_KEY` | nothing yet | reserved; `internal/agent` uses a deterministic keyword planner, no LLM call exists |
| `NEXT_PUBLIC_API_URL` | frontend | default `http://localhost:8080` |

## Third-party services

- **LiveKit** — realtime audio transport. The Go backend joins LiveKit
  rooms directly as a raw participant via `server-sdk-go/v2`'s
  `pkg/media` (no Go support exists for LiveKit's Agents framework).
  Requires `libopus`/`pkg-config` on the build machine (a cgo dependency
  of that SDK) — see Limitations.
- **Rime** — text-to-speech, called exactly once per voice turn, only
  after that turn's response has passed the Policy staleness check. See
  `RIME_EVIDENCE.md` for the full, source-verified configuration,
  acceptance test, and honestly-disclosed limitations (no live API call
  has been made in this development environment).
- **Deepgram** — speech-to-text, official `deepgram-go-sdk/v3`,
  prerecorded REST endpoint called once per locally-segmented utterance.

## Testing

```bash
cd backend
gofmt -l .            # should print nothing
go vet ./...           # excluding internal/voice/livekit and cmd/server
                        #   without libopus/pkg-config — see Limitations
go test ./...
go test -race ./...
go test -race -count=20 ./internal/state/... ./internal/tasks/... \
  ./internal/policy/... ./internal/agent/... ./internal/voice/...
go test -run '^$' -bench . ./internal/policy/... ./internal/tasks/... ./internal/voice/...

cd ../frontend
npx tsc --noEmit
npm run build
npx eslint .
npm test
```

## Demo

See `DEMO.md` for a rehearsed, 4-5 minute walkthrough of both core
scenarios (stale-result rejection, voice interruption) with exact UI
steps and equivalent `curl`/`go test` commands for a non-UI or no-voice-
credentials run-through.

## Limitations & failure behavior

- **`internal/voice/livekit` and `cmd/server` require `libopus` and
  `pkg-config`** on the build machine (a cgo dependency pulled in by
  `server-sdk-go/v2/pkg/media`'s Opus encode/decode). Every other
  package, including `internal/voice`'s own core logic, builds and tests
  without them. `cmd/devserver` exists specifically to develop and
  demo everything except real LiveKit audio without this dependency.
- **No database.** `DATABASE_URL`/PostgreSQL are provisioned in
  `docker-compose.yml` but no Go code opens a connection — `state.Store`,
  `tasks.Store`, `voice.Manager`, and `internal/activity` are all
  in-memory only. Restarting the backend loses all machines/tasks/history.
- **No real LLM.** `internal/agent.KeywordPlanner` is a deterministic
  keyword matcher, not a language model — see `internal/agent`'s doc
  comment for exactly which phrases it recognizes.
- **Rime has no fallback.** If a `Synthesize` call fails (bad key,
  network error, malformed response), that voice turn ends with no
  spoken output and no retry — see `RIME_EVIDENCE.md` §15.
- **No live Rime/LiveKit/Deepgram call has been exercised in this
  development environment** — no API credentials are configured here.
  Every claim above about these integrations is verified by reading the
  actual client code and running it against fakes/mocks, not against the
  real services; see `RIME_EVIDENCE.md`'s Limitations section for the
  precise boundary of what was and wasn't tested.
- **A true simultaneous tie** between "a tool finished" and "the user
  interrupted" is not guaranteed to always resolve the same way — see
  `TestRace_InterruptVsToolCompletion`'s doc comment in
  `internal/voice/interruption_test.go`. What is guaranteed: no data
  race, no panic, and the task engine always reaches exactly one
  consistent terminal state.
