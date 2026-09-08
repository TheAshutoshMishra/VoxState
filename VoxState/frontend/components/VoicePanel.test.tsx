import { describe, expect, it } from "vitest";
import { deriveTurns } from "./VoicePanel";

// deriveTurns is a pure reduction over this session's turn_* activity log
// lines (see the doc comment in VoicePanel.tsx) — no network, no DOM,
// exercised directly rather than through the connected component, which
// requires a real LiveKit room.
describe("deriveTurns", () => {
  const sessionId = "sess-1";

  it("returns nothing for a session with no turn activity", () => {
    expect(deriveTurns([], sessionId)).toEqual([]);
  });

  it("ignores entries for other sessions", () => {
    const entries = [
      { message: "turn_started", time: "t1", attrs: { session_id: "other", turn_id: "a" } },
    ];
    expect(deriveTurns(entries, sessionId)).toEqual([]);
  });

  it("marks a turn ACTIVE once started, ordered oldest-first", () => {
    const entries = [
      { message: "turn_started", time: "t2", attrs: { session_id: sessionId, turn_id: "b" } },
      { message: "turn_started", time: "t1", attrs: { session_id: sessionId, turn_id: "a" } },
    ];
    expect(deriveTurns(entries, sessionId)).toEqual([
      { id: "a", status: "ACTIVE", startedAt: "t1" },
      { id: "b", status: "ACTIVE", startedAt: "t2" },
    ]);
  });

  it("marks a turn COMPLETED once turn_completed follows turn_started", () => {
    const entries = [
      { message: "turn_started", time: "t1", attrs: { session_id: sessionId, turn_id: "a" } },
      { message: "turn_completed", time: "t2", attrs: { session_id: sessionId, turn_id: "a" } },
    ];
    expect(deriveTurns(entries, sessionId)).toEqual([
      { id: "a", status: "COMPLETED", startedAt: "t1" },
    ]);
  });

  it("marks a turn INTERRUPTED and keeps it that way even if turn_completed arrives after", () => {
    const entries = [
      { message: "turn_started", time: "t1", attrs: { session_id: sessionId, turn_id: "a" } },
      { message: "turn_interrupted", time: "t2", attrs: { session_id: sessionId, turn_id: "a" } },
      { message: "turn_completed", time: "t3", attrs: { session_id: sessionId, turn_id: "a" } },
    ];
    expect(deriveTurns(entries, sessionId)).toEqual([
      { id: "a", status: "INTERRUPTED", startedAt: "t1" },
    ]);
  });

  it("preserves the original startedAt when later events for the same turn arrive", () => {
    const entries = [
      { message: "turn_started", time: "t1", attrs: { session_id: sessionId, turn_id: "a" } },
      { message: "turn_interrupted", time: "t9", attrs: { session_id: sessionId, turn_id: "a" } },
    ];
    const [turn] = deriveTurns(entries, sessionId);
    expect(turn.startedAt).toBe("t1");
  });
});
