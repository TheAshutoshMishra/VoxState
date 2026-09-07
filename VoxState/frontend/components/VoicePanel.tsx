"use client";

import { useEffect, useRef, useState } from "react";
import { Room, RoomEvent, ConnectionState } from "livekit-client";
import { api, ApiError } from "@/lib/api";
import { usePoll } from "@/lib/usePoll";
import type { VoiceSessionResponse } from "@/lib/types";
import { Card } from "./Card";
import { ErrorBanner } from "./ErrorBanner";
import { StatusPill } from "./StatusPill";
import { Button } from "./Field";

const TURN_MESSAGES = new Set([
  "turn_started",
  "turn_interrupted",
  "turn_completed",
  "response_cancelled",
]);

interface Turn {
  id: string;
  status: "ACTIVE" | "INTERRUPTED" | "COMPLETED";
  startedAt: string;
}

// deriveTurns reduces this session's turn_* activity log lines (see
// internal/voice/session.go's M7 logging) into one row per turn — the
// backend is still the sole source of truth for what happened; this only
// groups/orders what it already reported. Exported for the component
// test in VoicePanel.test.tsx.
export function deriveTurns(
  entries: { message: string; time: string; attrs?: Record<string, unknown> }[],
  sessionId: string,
): Turn[] {
  const bySessions = entries.filter(
    (e) => TURN_MESSAGES.has(e.message) && e.attrs?.session_id === sessionId,
  );
  const byTurn = new Map<string, Turn>();
  for (const e of [...bySessions].sort((a, b) => a.time.localeCompare(b.time))) {
    const turnId = String(e.attrs?.turn_id ?? "");
    if (!turnId) continue;
    const existing = byTurn.get(turnId);
    const startedAt = existing?.startedAt ?? e.time;
    let status: Turn["status"] = existing?.status ?? "ACTIVE";
    if (e.message === "turn_interrupted") status = "INTERRUPTED";
    else if (e.message === "turn_completed" && status !== "INTERRUPTED") status = "COMPLETED";
    byTurn.set(turnId, { id: turnId, status, startedAt });
  }
  return [...byTurn.values()].sort((a, b) => a.startedAt.localeCompare(b.startedAt));
}

function turnVariant(status: Turn["status"]) {
  if (status === "ACTIVE") return "info" as const;
  if (status === "INTERRUPTED") return "danger" as const;
  return "success" as const;
}

export function VoicePanel({ machineId }: { machineId: string }) {
  const [session, setSession] = useState<VoiceSessionResponse | null>(null);
  const [connectionState, setConnectionState] = useState<ConnectionState | "idle">("idle");
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const roomRef = useRef<Room | null>(null);

  const { data: activity } = usePoll(() => api.listActivity(), "activity");

  useEffect(() => {
    return () => {
      roomRef.current?.disconnect();
    };
  }, []);

  async function handleConnect() {
    if (!machineId) return;
    setConnecting(true);
    setError(null);
    try {
      const sess = await api.startVoiceSession(machineId, "dashboard-user");
      setSession(sess);

      const room = new Room();
      roomRef.current = room;
      room.on(RoomEvent.ConnectionStateChanged, (state) => setConnectionState(state));
      room.on(RoomEvent.Disconnected, () => setConnectionState(ConnectionState.Disconnected));

      try {
        await room.connect(sess.livekit_url, sess.livekit_token);
        await room.localParticipant.setMicrophoneEnabled(true);
      } catch (liveKitErr) {
        // The session lifecycle over HTTP still succeeded — only the
        // realtime media connection failed (e.g. no real LiveKit server
        // behind this URL, or the browser denied mic access). Surface it
        // distinctly rather than pretending the whole action failed.
        setError(
          `Voice session started, but the realtime connection failed: ${
            liveKitErr instanceof Error ? liveKitErr.message : String(liveKitErr)
          }`,
        );
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to start voice session");
    } finally {
      setConnecting(false);
    }
  }

  async function handleDisconnect() {
    if (!session) return;
    setError(null);
    try {
      roomRef.current?.disconnect();
      roomRef.current = null;
      await api.endVoiceSession(session.session_id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Failed to end voice session");
    } finally {
      setSession(null);
      setConnectionState("idle");
    }
  }

  const turns = session && activity ? deriveTurns(activity, session.session_id) : [];
  const activeTurn = [...turns].reverse().find((t) => t.status === "ACTIVE");

  return (
    <Card title="Voice Session" subtitle="Realtime transport reuses the existing M6/M7 LiveKit architecture">
      <div className="flex items-center gap-2">
        <StatusPill
          label={
            !session
              ? "DISCONNECTED"
              : connectionState === ConnectionState.Connected
                ? "CONNECTED"
                : connectionState === "idle"
                  ? "SESSION STARTED"
                  : String(connectionState).toUpperCase()
          }
          variant={connectionState === ConnectionState.Connected ? "success" : session ? "warning" : "neutral"}
        />
        {!session ? (
          <Button variant="primary" onClick={handleConnect} disabled={connecting || !machineId}>
            {connecting ? "Connecting…" : "Connect Voice"}
          </Button>
        ) : (
          <Button variant="danger" onClick={handleDisconnect}>
            Disconnect
          </Button>
        )}
      </div>

      {error && <ErrorBanner message={error} />}

      {session && (
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
          <dt className="text-zinc-500">Session ID</dt>
          <dd className="font-mono text-zinc-200">{session.session_id}</dd>
          <dt className="text-zinc-500">Machine</dt>
          <dd className="text-zinc-200">{session.machine_id}</dd>
          <dt className="text-zinc-500">Room</dt>
          <dd className="font-mono text-zinc-200">{session.livekit_room_id}</dd>
        </dl>
      )}

      {session && (
        <div className="flex flex-col gap-1.5 border-t border-zinc-800 pt-3">
          <span className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
            Turns
          </span>
          {turns.length === 0 ? (
            <p className="text-xs text-zinc-500">
              No turns yet — speak once connected, or interrupt an in-progress response to see it here.
            </p>
          ) : (
            <ul className="flex flex-col gap-1">
              {turns.map((t) => (
                <li key={t.id} className="flex items-center gap-2 text-xs">
                  <span className="font-mono text-zinc-400">Turn {t.id}</span>
                  <StatusPill label={t.status} variant={turnVariant(t.status)} />
                </li>
              ))}
            </ul>
          )}
          {activeTurn && (
            <p className="text-xs text-sky-400">
              Turn {activeTurn.id} is currently active — talking over it will interrupt it.
            </p>
          )}
        </div>
      )}
    </Card>
  );
}
