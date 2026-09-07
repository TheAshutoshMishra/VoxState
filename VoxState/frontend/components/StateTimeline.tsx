"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { usePoll } from "@/lib/usePoll";
import { formatDateTime } from "@/lib/format";
import type { StateResponse } from "@/lib/types";
import { Card } from "./Card";
import { ErrorBanner } from "./ErrorBanner";

// StateTimeline fetches every historical MachineState version 1..current
// via GET /machines/{id}/state?version=N (the only per-version endpoint
// the backend exposes — there is no "list all versions" endpoint) and
// renders them oldest-first. Real data only: it never fabricates a
// version it didn't actually fetch.
export function StateTimeline({ machineId }: { machineId: string }) {
  const { data: current } = usePoll(
    () => api.getState(machineId),
    machineId,
  );
  const [versions, setVersions] = useState<StateResponse[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!machineId || !current) return;
    let cancelled = false;
    (async () => {
      try {
        const fetched = await Promise.all(
          Array.from({ length: current.version }, (_, i) =>
            api.getState(machineId, i + 1),
          ),
        );
        if (!cancelled) {
          setVersions(fetched);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof ApiError ? err.message : "Failed to load version history");
        }
      }
    })();
    return () => {
      cancelled = true;
    };
    // Deliberately depends on current?.version, not the whole `current`
    // object: usePoll returns a new object every 2s poll even when the
    // version hasn't changed, and refetching all N versions on every
    // unrelated poll tick would be wasted work for no visible benefit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [machineId, current?.version]);

  if (!machineId) return null;

  return (
    <Card title="State Version Timeline" subtitle="Older versions are immutable — the backend never rewrites history">
      {error && <ErrorBanner message={error} />}
      {versions.length === 0 ? (
        <p className="text-sm text-zinc-500">No versions yet.</p>
      ) : (
        <ol className="flex flex-col gap-0">
          {versions.map((v, i) => {
            const isCurrent = current && v.version === current.version;
            return (
              <li key={v.version} className="relative flex gap-3 pb-4 last:pb-0">
                {i < versions.length - 1 && (
                  <span
                    aria-hidden="true"
                    className="absolute left-[9px] top-5 h-full w-px bg-zinc-700"
                  />
                )}
                <span
                  aria-hidden="true"
                  className={`mt-1 h-[18px] w-[18px] shrink-0 rounded-full border-2 ${
                    isCurrent
                      ? "border-sky-400 bg-sky-950"
                      : "border-zinc-600 bg-zinc-900"
                  }`}
                />
                <div className="flex flex-col">
                  <span className="font-mono text-sm font-semibold text-zinc-100">
                    v{v.version}
                    {isCurrent && (
                      <span className="ml-2 rounded bg-sky-900 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-sky-300">
                        Current
                      </span>
                    )}
                  </span>
                  <span className="text-xs text-zinc-500">
                    {formatDateTime(v.created_at)} · status: {v.status}
                  </span>
                  {v.attributes && Object.keys(v.attributes).length > 0 && (
                    <span className="text-xs text-zinc-400">
                      {Object.entries(v.attributes)
                        .map(([k, val]) => `${k}=${val}`)
                        .join(", ")}
                    </span>
                  )}
                </div>
              </li>
            );
          })}
        </ol>
      )}
    </Card>
  );
}
