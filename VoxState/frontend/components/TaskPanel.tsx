"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { useActivityLog } from "@/lib/activityLog";
import { usePoll } from "@/lib/usePoll";
import { formatTime } from "@/lib/format";
import { Card } from "./Card";
import { ErrorBanner } from "./ErrorBanner";
import { StatusPill, taskStatusVariant } from "./StatusPill";
import { Button, Select } from "./Field";

const TOOLS = ["vibration_scan", "temperature_scan"];

export function TaskPanel({ machineId }: { machineId: string }) {
  const { push } = useActivityLog();
  const {
    data: tasks,
    error,
    loading,
    refetch,
  } = usePoll(() => api.listTasks(machineId), machineId);
  const { data: state } = usePoll(() => api.getState(machineId), machineId);

  const [tool, setTool] = useState(TOOLS[0]);
  const [creating, setCreating] = useState(false);
  const [busyTaskId, setBusyTaskId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  if (!machineId) {
    return (
      <Card title="Tasks">
        <p className="text-sm text-zinc-500">No machine selected.</p>
      </Card>
    );
  }

  async function handleCreate() {
    setCreating(true);
    setActionError(null);
    try {
      const res = await api.createTask(machineId, tool);
      push({
        message: res.event.type,
        time: res.event.created_at,
        attrs: { task_id: res.task.id, bound_version: res.task.bound_version },
      });
      refetch();
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : "Failed to create task");
    } finally {
      setCreating(false);
    }
  }

  async function handleStart(taskId: string) {
    setBusyTaskId(taskId);
    setActionError(null);
    try {
      await api.startTask(taskId, 15);
      refetch();
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : "Failed to start task");
    } finally {
      setBusyTaskId(null);
    }
  }

  async function handleCancel(taskId: string) {
    setBusyTaskId(taskId);
    setActionError(null);
    try {
      const res = await api.cancelTask(taskId);
      push({ message: res.event.type, time: res.event.created_at, attrs: { task_id: res.task.id } });
      refetch();
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : "Failed to cancel task");
    } finally {
      setBusyTaskId(null);
    }
  }

  return (
    <Card
      title="Diagnostic Tasks"
      subtitle="Each task is bound, once and forever, to the state version current when it was created"
      actions={
        <div className="flex items-center gap-2">
          <Select value={tool} onChange={(e) => setTool(e.target.value)} aria-label="Tool to run">
            {TOOLS.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </Select>
          <Button variant="primary" onClick={handleCreate} disabled={creating}>
            {creating ? "Creating…" : "+ New Task"}
          </Button>
        </div>
      }
    >
      {error && <ErrorBanner message={error} onRetry={refetch} />}
      {actionError && <ErrorBanner message={actionError} />}
      {loading && !tasks ? (
        <p className="text-sm text-zinc-500">Loading tasks…</p>
      ) : !tasks || tasks.length === 0 ? (
        <p className="text-sm text-zinc-500">No tasks yet for this machine.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {[...tasks].reverse().map((t) => {
            const stale = state && t.bound_version !== state.version;
            return (
              <li
                key={t.id}
                className="flex flex-col gap-1.5 rounded border border-zinc-800 bg-zinc-900/60 p-2.5 text-sm"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="font-mono text-xs text-zinc-400">{t.id}</span>
                  <StatusPill label={t.status} variant={taskStatusVariant(t.status)} />
                </div>
                <div className="flex flex-wrap items-center gap-3 text-xs">
                  <span className="text-zinc-400">Tool: <span className="text-zinc-100">{t.tool_name}</span></span>
                  <span className="text-zinc-400">Created: {formatTime(t.created_at)}</span>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="rounded border border-zinc-700 bg-zinc-950 px-2 py-0.5 font-mono text-xs">
                    Task Bound Version: v{t.bound_version}
                  </span>
                  <span className="rounded border border-zinc-700 bg-zinc-950 px-2 py-0.5 font-mono text-xs">
                    Machine Current Version: v{state?.version ?? "?"}
                  </span>
                  {state && (
                    <StatusPill
                      label={stale ? "STALE" : "MATCH"}
                      variant={stale ? "danger" : "success"}
                    />
                  )}
                </div>
                {t.status === "PENDING" && (
                  <div>
                    <Button onClick={() => handleStart(t.id)} disabled={busyTaskId === t.id}>
                      {busyTaskId === t.id ? "Starting…" : "Start"}
                    </Button>
                  </div>
                )}
                {t.status === "RUNNING" && (
                  <div>
                    <Button variant="danger" onClick={() => handleCancel(t.id)} disabled={busyTaskId === t.id}>
                      {busyTaskId === t.id ? "Cancelling…" : "Cancel"}
                    </Button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}
