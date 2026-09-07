"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { useActivityLog } from "@/lib/activityLog";
import { usePoll } from "@/lib/usePoll";
import { formatDateTime } from "@/lib/format";
import { Card } from "./Card";
import { ErrorBanner } from "./ErrorBanner";
import { StatusPill } from "./StatusPill";
import { Button, Label, Select, TextInput } from "./Field";
import { KeyValueEditor, type KeyValuePair } from "./KeyValueEditor";

const STATUS_OPTIONS = ["running", "fault", "under_maintenance", "stopped"];

export function MachinePanel({ machineId }: { machineId: string }) {
  const { push } = useActivityLog();
  const {
    data: state,
    error,
    loading,
    refetch,
  } = usePoll(
    () => api.getState(machineId),
    machineId,
  );
  const { data: machine } = usePoll(
    () => api.getMachine(machineId),
    machineId,
    5000,
  );

  const [status, setStatus] = useState("");
  const [attrs, setAttrs] = useState<KeyValuePair[]>([]);
  const [source, setSource] = useState("system");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  if (!machineId) {
    return (
      <Card title="Machine">
        <p className="text-sm text-zinc-500">
          No machine selected. Create one above to get started.
        </p>
      </Card>
    );
  }

  async function handleChangeState(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setFormError(null);
    try {
      const attributes = Object.fromEntries(attrs.map((p) => [p.key, p.value]));
      const res = await api.changeState(machineId, {
        status: status || undefined,
        attributes: Object.keys(attributes).length > 0 ? attributes : undefined,
        source,
        note: note.trim() || undefined,
      });
      for (const ev of res.events ?? []) {
        push({ message: ev.type, time: ev.created_at, attrs: { machine_id: machineId, ...ev.payload } });
      }
      setAttrs([]);
      setNote("");
      refetch();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "Failed to change state");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Card
      title="Machine"
      subtitle={machine ? `${machine.name} · ${machine.id}` : machineId}
    >
      {error && <ErrorBanner message={error} onRetry={refetch} />}
      {loading && !state ? (
        <p className="text-sm text-zinc-500">Loading current state…</p>
      ) : state ? (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <StatusPill
              label={state.status.toUpperCase()}
              variant={
                state.status === "fault"
                  ? "danger"
                  : state.status === "under_maintenance"
                    ? "warning"
                    : "success"
              }
            />
            <span className="rounded border border-zinc-700 bg-zinc-900 px-2 py-0.5 font-mono text-xs text-zinc-200">
              Current State Version: v{state.version}
            </span>
          </div>
          {state.attributes && Object.keys(state.attributes).length > 0 && (
            <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3">
              {Object.entries(state.attributes).map(([k, v]) => (
                <div key={k} className="flex justify-between gap-2 rounded bg-zinc-900 px-2 py-1">
                  <dt className="text-zinc-500">{k}</dt>
                  <dd className="font-medium text-zinc-100">{v}</dd>
                </div>
              ))}
            </dl>
          )}
          <p className="text-xs text-zinc-500">
            Last state change: {formatDateTime(state.created_at)}
          </p>
        </div>
      ) : null}

      <form onSubmit={handleChangeState} className="flex flex-col gap-2 border-t border-zinc-800 pt-3">
        <span className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
          Change State
        </span>
        <div className="flex flex-wrap items-end gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="status-select">Status</Label>
            <Select id="status-select" value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="">(unchanged)</option>
              {STATUS_OPTIONS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="source-select">Source</Label>
            <Select id="source-select" value={source} onChange={(e) => setSource(e.target.value)}>
              <option value="system">system</option>
              <option value="technician">technician</option>
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="note-input">Note</Label>
            <TextInput
              id="note-input"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Motor replaced"
            />
          </div>
        </div>
        <KeyValueEditor pairs={attrs} onChange={setAttrs} label="Attribute changes" />
        <div>
          <Button type="submit" variant="primary" disabled={submitting}>
            {submitting ? "Applying…" : "Apply State Change"}
          </Button>
        </div>
        {formError && <ErrorBanner message={formError} />}
      </form>
    </Card>
  );
}
