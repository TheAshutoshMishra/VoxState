# VoxState demo script (4-5 minutes)

Deterministic, rehearsable walkthrough of the version-fencing / stale-result
protection architecture — the thing this project actually proves, not a
generic chatbot feature tour.

## Setup (before the audience arrives)

```bash
cd backend
go run ./cmd/server     # real LiveKit/Rime/Deepgram — needs credentials, see .env.example
# or, without libopus/pkg-config or voice credentials:
go run ./cmd/devserver  # identical API, in-memory voice fakes (no real audio)

cd frontend
npm run dev             # http://localhost:3000
```

Have the frontend open, no machine selected yet.

## Part 1 — Stale-result rejection (≈2.5 min)

1. **Introduce Machine 17.** In the "+ New Machine" form, create
   `machine-17` / "Machine 17". The dashboard now shows `MachinePanel`
   with **Current State Version: v1**.
2. **Show current state/version.** Point at the version badge and
   `StateTimeline`, which shows exactly one entry, v1, marked "Current".
3. **Start a diagnostic bound to version N.** In `PolicyPanel`, click
   **"Run Guided Stale-Result Demo"**. Narrate step 1 as it appears:
   "Machine is at v1." then "Started diagnostic ..., bound to v1." —
   `TaskPanel` now shows a new task with `Task Bound Version: v1` and a
   green `MATCH` pill (machine is still at v1).
4. **Change machine state to N+1 while the diagnostic is running.** The
   same guided-demo click already does this next: watch the log line
   "Machine changed to v2." appear, `MachinePanel`'s version badge flips
   to v2, and `StateTimeline` gains a new v2 entry.
5. **Let the old diagnostic finish.** The guided demo immediately submits
   that task's ("stale") result.
6. **Show Policy rejecting the stale result.** The log line reads
   `Diagnostic "completed" with its stale reading. Policy: REJECTED.` —
   point at the red `REJECTED — STALE RESULT` pill and the reason text:
   *"Result discarded because machine state changed."* This verdict is
   the backend's own `POST /tasks/{id}/result` response, not anything
   computed in the browser (see `docs/ARCHITECTURE.md`'s "M8: backend
   authority" note).
7. **Show that stale information does not reach spoken output.** Switch
   to `PolicyPanel`'s "Run Agent" control and run the same instruction
   ("check vibration") end to end: `agentResult.outcome` is what a real
   voice turn would gate speech on (`internal/voice/session.go`'s
   `handleUtterance` only calls Rime after this exact check passes) — a
   rejected result never has a `payload` field at all, so there is
   nothing for Rime to be handed. For the fully live version of this same
   point (no frontend, no clicking): run
   ```bash
   curl -X POST localhost:8080/machines/machine-17/agent/run -d '{"instruction":"check vibration"}' &
   sleep 1
   curl -X POST localhost:8080/machines/machine-17/state -d '{"status":"fault"}'
   wait
   ```
   and read the agent's own JSON response aloud: `"outcome":"REJECTED"`,
   `"replan_required":true`, no `"payload"` key present.

## Part 2 — Voice interruption (≈2 min)

8. **Start a voice interaction.** In `VoicePanel`, click **Connect
   Voice** (requires real `LIVEKIT_URL`/`LIVEKIT_API_KEY`/
   `LIVEKIT_API_SECRET` and a browser with mic access; against
   `cmd/devserver` the session lifecycle still works but there is no real
   audio track). Speak an instruction such as "check vibration" — a Turn
   row appears in `VoicePanel`, `ACTIVE`.
9. **Interrupt the agent.** While the agent is still working/speaking,
   speak over it. The active turn's row flips to `INTERRUPTED` and a new
   turn begins.
10. **Show Turn A cancellation/invalidation.** Point at `ActivityStream`:
    `turn_interrupted` for turn A, followed by `UserInterrupted` and
    `ResponseInvalidated` events, and `TaskPanel` showing turn A's task
    move to `CANCELLED`.
11. **Show Turn B becoming authoritative.** The new turn's row appears
    `ACTIVE`, then `COMPLETED`; `ActivityStream` shows exactly one
    `turn_completed` for it, and only its content ever reached TTS (this
    exact scenario, scripted without needing a live microphone, is
    `TestFullDuplex_InterruptionThenNewInstructionBecomesAuthoritative`
    in `backend/internal/voice/interruption_test.go` — worth running
    live in front of the audience: `go test -v -run TestFullDuplex ./internal/voice/`).
12. **Explain why this matters.** Close on the throughline: an AI voice
    agent that speaks whatever a background tool eventually returns can
    confidently state something that stopped being true seconds ago, or
    keep talking after the user already said "stop." VoxState makes both
    failure modes structurally impossible — not by convention or prompt
    instruction, but by binding every task to an immutable state version
    at creation and gating every result through one policy layer before
    it can ever reach speech.

## What this demo deliberately does not claim

- No live Rime/LiveKit/Deepgram call has been exercised in the
  environment this was verified in — see `RIME_EVIDENCE.md`'s
  Limitations section. Steps 8-11 as scripted (`TestFullDuplex...`) use
  in-memory fakes for STT/TTS/transport, which is what makes them
  deterministic and rehearsable without depending on network conditions
  on demo day; running them against real LiveKit/Rime/Deepgram credentials
  is the live version of the same scenario, not a different one.
- Reasoning/tool-selection uses a deterministic keyword planner
  (`internal/agent.KeywordPlanner`), not an LLM — this demo is about the
  version-fencing architecture, which is independent of what selects the
  tool.
