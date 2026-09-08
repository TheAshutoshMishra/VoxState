# VoxState frontend

Next.js/React/TypeScript control surface for the VoxState backend
(`../backend`). See `../docs/ARCHITECTURE.md`'s "Frontend" section and
"M8: backend authority" note, and `../CLAUDE.md`'s M8 milestone log entry,
for the full design rationale. In short: this app displays and drives
machine/state/task/policy/voice data, but it never decides
ACCEPTED/REJECTED/STALE itself — every such verdict comes verbatim from
the Go backend.

## Setup

```bash
npm install
cp .env.example .env.local   # NEXT_PUBLIC_API_URL, defaults to http://localhost:8080
```

## Running

Start the backend first (see `../backend`'s own instructions, or
`../CLAUDE.md`'s "Commands" section). If `libopus`/`pkg-config` aren't
installed locally, use `cd ../backend && go run ./cmd/devserver` instead
of `cmd/server` — it serves the identical API with in-memory voice-
provider fakes, sufficient for every panel except real LiveKit audio.

```bash
npm run dev
```

Open http://localhost:3000.

## Commands

```bash
npm run build   # production build (also type-checks)
npm run lint    # eslint
npm test        # vitest run (jsdom, component-level unit tests)
```

## Structure

- `app/` — Next.js App Router entry point (`page.tsx` lays out the panel grid)
- `components/` — one component per panel (`MachinePanel`, `StateTimeline`,
  `TaskPanel`, `PolicyPanel`, `VoicePanel`, `ActivityStream`) plus shared
  primitives (`Card`, `Field`, `StatusPill`, `ErrorBanner`, ...)
- `lib/api.ts` — typed fetch wrapper; one function per backend route, no
  business logic
- `lib/usePoll.ts` — the app's only "framework": polls a fetch function on
  an interval with manual refetch, used instead of a websocket/SSE layer
- `lib/types.ts` — response types mirroring the backend's JSON shapes
