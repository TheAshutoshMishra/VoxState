"use client";

import { api, API_BASE_URL } from "@/lib/api";
import { usePoll } from "@/lib/usePoll";
import { StatusPill } from "./StatusPill";

export function BackendStatus() {
  const { data, error } = usePoll(() => api.health(), "health", 5000);

  if (error) {
    return (
      <div className="flex items-center gap-2" role="status">
        <StatusPill label="BACKEND UNAVAILABLE" variant="danger" />
        <span className="text-xs text-zinc-500">
          Could not reach {API_BASE_URL}. Is the Go backend running?
        </span>
      </div>
    );
  }

  return (
    <div role="status">
      <StatusPill label={data ? "BACKEND CONNECTED" : "CONNECTING…"} variant={data ? "success" : "neutral"} />
    </div>
  );
}
