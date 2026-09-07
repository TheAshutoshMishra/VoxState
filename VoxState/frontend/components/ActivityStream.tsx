"use client";

import { api } from "@/lib/api";
import { usePoll } from "@/lib/usePoll";
import { useActivityLog } from "@/lib/activityLog";
import { formatTime } from "@/lib/format";
import { Card } from "./Card";
import { ErrorBanner } from "./ErrorBanner";

interface FeedItem {
  message: string;
  time: string;
  attrs?: Record<string, unknown>;
  source: "backend" | "local";
}

const EVENT_LABELS: Record<string, string> = {
  MachineCreated: "MachineCreated",
  MachineStateChanged: "MachineStateChanged",
  TechnicianReported: "TechnicianReported",
  DiagnosticStarted: "DiagnosticStarted",
  DiagnosticCancelled: "DiagnosticCancelled",
  DiagnosticCompleted: "DiagnosticCompleted",
  ToolResultRejected: "ToolResultRejected",
  UserInterrupted: "UserInterrupted",
  ResponseInvalidated: "ResponseInvalidated",
};

function summarize(item: FeedItem): string {
  const a = item.attrs ?? {};
  const machine = a.machine_id ? ` Machine ${a.machine_id}` : "";
  switch (item.message) {
    case "MachineStateChanged":
      return `${machine} → v${a.resulting_state_version ?? "?"}`;
    case "DiagnosticStarted":
      return `Task ${a.task_id ?? "?"} bound to v${a.bound_version ?? "?"}`;
    case "DiagnosticCancelled":
      return `Task ${a.task_id ?? "?"} cancelled`;
    case "DiagnosticCompleted":
      return `Task ${a.task_id ?? "?"} completed`;
    case "ToolResultRejected":
      return `stale result (bound v${a.result_version ?? "?"} vs current v${a.current_version ?? "?"})`;
    case "turn_started":
      return `Turn ${a.turn_id ?? "?"} started`;
    case "turn_interrupted":
      return `Turn ${a.turn_id ?? "?"} interrupted`;
    case "turn_completed":
      return `Turn ${a.turn_id ?? "?"} completed (${a.outcome ?? "?"})`;
    case "response_cancelled":
      return `Turn ${a.turn_id ?? "?"} response cancelled`;
    case "audio_stopped":
      return `Turn ${a.turn_id ?? "?"} audio stopped`;
    default:
      return machine.trim();
  }
}

export function ActivityStream() {
  const { data: backendEntries, error, refetch } = usePoll(
    () => api.listActivity(),
    "activity-stream",
  );
  const { localEvents } = useActivityLog();

  const feed: FeedItem[] = [
    ...(backendEntries ?? []).map((e) => ({ ...e, source: "backend" as const })),
    ...localEvents.map((e) => ({ ...e, source: "local" as const })),
  ].sort((a, b) => b.time.localeCompare(a.time));

  return (
    <Card title="Activity Stream" subtitle="Real backend events, newest first">
      {error && <ErrorBanner message={error} onRetry={refetch} />}
      {feed.length === 0 ? (
        <p className="text-sm text-zinc-500">No activity yet. Try the demo controls.</p>
      ) : (
        <ul className="flex max-h-96 flex-col gap-1 overflow-y-auto font-mono text-xs">
          {feed.map((item, i) => (
            <li key={`${item.source}-${item.time}-${i}`} className="flex gap-2 border-b border-zinc-900 py-1">
              <span className="shrink-0 text-zinc-600">{formatTime(item.time)}</span>
              <span className="shrink-0 font-semibold text-zinc-200">
                {EVENT_LABELS[item.message] ?? item.message}
              </span>
              <span className="text-zinc-500">{summarize(item)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
