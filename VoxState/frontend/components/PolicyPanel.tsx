"use client";

import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { useActivityLog } from "@/lib/activityLog";
import { usePoll } from "@/lib/usePoll";
import type { AgentResultResponse, DecisionResponse } from "@/lib/types";
import { Card } from "./Card";
import { ErrorBanner } from "./ErrorBanner";
import { StatusPill } from "./StatusPill";
import { Button, Label, Select, TextInput } from "./Field";

// PolicyPanel visualizes the single most important comparison in
// VoxState: a task's bound state version vs the machine's current one,
// and the backend's own ACCEPTED/REJECTED verdict — never recomputed
// here (see docs/ARCHITECTURE.md's M8 "backend authority" note).
export function PolicyPanel({ machineId }: { machineId: string }) {
  const { push } = useActivityLog();
  const { data: tasks, refetch: refetchTasks } = usePoll(
    () => api.listTasks(machineId),
    machineId,
  );

  const [selectedTaskId, setSelectedTaskId] = useState("");
  const [decision, setDecision] = useState<DecisionResponse | null>(null);
  const [evaluating, setEvaluating] = useState(false);
  const [evalError, setEvalError] = useState<string | null>(null);

  const [instruction, setInstruction] = useState("check vibration");
  const [agentResult, setAgentResult] = useState<AgentResultResponse | null>(null);
  const [agentRunning, setAgentRunning] = useState(false);
  const [agentError, setAgentError] = useState<string | null>(null);

  const [demoRunning, setDemoRunning] = useState(false);
  const [demoError, setDemoError] = useState<string | null>(null);
  const [demoSteps, setDemoSteps] = useState<string[]>([]);

  const effectiveTaskId = selectedTaskId || tasks?.[tasks.length - 1]?.id || "";

  async function handleEvaluate() {
    if (!effectiveTaskId) return;
    setEvaluating(true);
    setEvalError(null);
    try {
      const res = await api.simulateResult(effectiveTaskId, { demo: "reading" });
      setDecision(res);
      if (res.event) {
        push({ message: res.event.type, time: res.event.created_at, attrs: res.event.payload });
      }
    } catch (err) {
      setEvalError(err instanceof ApiError ? err.message : "Failed to evaluate result");
    } finally {
      setEvaluating(false);
    }
  }

  async function handleRunAgent() {
    setAgentRunning(true);
    setAgentError(null);
    setAgentResult(null);
    try {
      const res = await api.runAgent(machineId, instruction);
      setAgentResult(res);
      refetchTasks();
    } catch (err) {
      setAgentError(err instanceof ApiError ? err.message : "Agent run failed");
    } finally {
      setAgentRunning(false);
    }
  }

  // The M8 acceptance demo (brief §18): create a task bound to the
  // current version, change the machine's state so the task is now
  // stale, then evaluate a simulated result for it — showing the exact
  // REJECTED / "result discarded because machine state changed" path,
  // deterministically and instantly (no need to wait for a real 3s tool).
  async function handleGuidedDemo() {
    setDemoRunning(true);
    setDemoError(null);
    setDemoSteps([]);
    try {
      const before = await api.getState(machineId);
      setDemoSteps((s) => [...s, `Machine is at v${before.version}.`]);

      const taskRes = await api.createTask(machineId, "vibration_scan");
      setDemoSteps((s) => [
        ...s,
        `Started diagnostic ${taskRes.task.id}, bound to v${taskRes.task.bound_version}.`,
      ]);
      push({ message: taskRes.event.type, time: taskRes.event.created_at, attrs: { task_id: taskRes.task.id } });

      // A fixed attribute value would be a no-op (and therefore NOT bump
      // the version — see state.Store.ChangeState's documented no-op
      // detection) if the machine already happens to be in that state,
      // e.g. from a previous run of this same demo. A unique nonce
      // guarantees a genuine, visible version bump every time the button
      // is clicked, regardless of prior state.
      const changeRes = await api.changeState(machineId, {
        attributes: { vibration: "CRITICAL", demo_run_at: String(Date.now()) },
        note: "Guided demo: simulate a state change while the diagnostic is bound to the old version",
      });
      setDemoSteps((s) => [...s, `Machine changed to v${changeRes.state.version}.`]);
      for (const ev of changeRes.events ?? []) {
        push({ message: ev.type, time: ev.created_at, attrs: { machine_id: machineId, ...ev.payload } });
      }

      const decisionRes = await api.simulateResult(taskRes.task.id, {
        vibration: "reading-from-stale-task",
      });
      setDemoSteps((s) => [
        ...s,
        `Diagnostic "completed" with its stale reading. Policy: ${decisionRes.outcome}.`,
      ]);
      if (decisionRes.event) {
        push({ message: decisionRes.event.type, time: decisionRes.event.created_at, attrs: decisionRes.event.payload });
      }
      setDecision(decisionRes);
      setSelectedTaskId(taskRes.task.id);
      refetchTasks();
    } catch (err) {
      setDemoError(err instanceof ApiError ? err.message : "Guided demo failed");
    } finally {
      setDemoRunning(false);
    }
  }

  if (!machineId) {
    return (
      <Card title="Policy / Consistency">
        <p className="text-sm text-zinc-500">No machine selected.</p>
      </Card>
    );
  }

  return (
    <Card
      title="Policy / Consistency"
      subtitle="The backend decides ACCEPTED vs REJECTED — this panel only displays its verdict"
    >
      <div className="rounded border border-sky-900 bg-sky-950/30 p-3">
        <p className="mb-2 text-sm text-zinc-300">
          One-click reproduction of VoxState&apos;s core scenario: bind a
          task to v<em>N</em>, change the machine to v<em>N+1</em>, then
          show the stale result get rejected.
        </p>
        <Button variant="primary" onClick={handleGuidedDemo} disabled={demoRunning}>
          {demoRunning ? "Running demo…" : "Run Guided Stale-Result Demo"}
        </Button>
        {demoError && <ErrorBanner message={demoError} />}
        {demoSteps.length > 0 && (
          <ol className="mt-2 flex flex-col gap-1 text-xs text-zinc-400">
            {demoSteps.map((s, i) => (
              <li key={i}>{i + 1}. {s}</li>
            ))}
          </ol>
        )}
      </div>

      <div className="flex flex-col gap-2 border-t border-zinc-800 pt-3">
        <span className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
          Manual: Evaluate a Task&apos;s Result
        </span>
        <div className="flex flex-wrap items-end gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="task-select">Task</Label>
            <Select
              id="task-select"
              value={effectiveTaskId}
              onChange={(e) => setSelectedTaskId(e.target.value)}
            >
              {!tasks || tasks.length === 0 ? (
                <option value="">No tasks yet</option>
              ) : (
                tasks.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.id} (bound v{t.bound_version})
                  </option>
                ))
              )}
            </Select>
          </div>
          <Button onClick={handleEvaluate} disabled={evaluating || !effectiveTaskId}>
            {evaluating ? "Evaluating…" : "Evaluate Result"}
          </Button>
        </div>
        {evalError && <ErrorBanner message={evalError} />}
        {decision && (
          <div className="flex flex-col gap-1.5 rounded border border-zinc-800 bg-zinc-900/60 p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2 font-mono text-xs">
              <span className="rounded border border-zinc-700 bg-zinc-950 px-2 py-0.5">
                Bound Version: v{decision.result.bound_version}
              </span>
              <span className="rounded border border-zinc-700 bg-zinc-950 px-2 py-0.5">
                Current Version: v{decision.current_version}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-zinc-500">Policy:</span>
              <StatusPill
                label={decision.outcome === "ACCEPTED" ? "ACCEPTED" : "REJECTED — STALE RESULT"}
                variant={decision.outcome === "ACCEPTED" ? "success" : "danger"}
              />
            </div>
            {decision.reason && (
              <p className="text-xs text-zinc-400">
                &ldquo;Result discarded because machine state changed.&rdquo; ({decision.reason})
              </p>
            )}
          </div>
        )}
      </div>

      <div className="flex flex-col gap-2 border-t border-zinc-800 pt-3">
        <span className="text-xs font-semibold uppercase tracking-wide text-zinc-500">
          Manual: Run Agent (real end-to-end loop)
        </span>
        <div className="flex flex-wrap items-end gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="instruction-input">Instruction</Label>
            <TextInput
              id="instruction-input"
              value={instruction}
              onChange={(e) => setInstruction(e.target.value)}
              placeholder="check vibration"
              className="w-64"
            />
          </div>
          <Button onClick={handleRunAgent} disabled={agentRunning}>
            {agentRunning ? "Running (real tool takes a few seconds)…" : "Run Agent"}
          </Button>
        </div>
        {agentError && <ErrorBanner message={agentError} />}
        {agentResult && (
          <div className="flex flex-col gap-1 rounded border border-zinc-800 bg-zinc-900/60 p-3 text-sm">
            <span className="text-xs text-zinc-400">
              Selected tool: <span className="text-zinc-100">{agentResult.selected_tool}</span>
            </span>
            <StatusPill
              label={agentResult.outcome}
              variant={agentResult.outcome === "ACCEPTED" ? "success" : "danger"}
            />
            {agentResult.outcome === "ACCEPTED" && agentResult.payload && (
              <pre className="overflow-x-auto rounded bg-zinc-950 p-2 text-xs text-zinc-300">
                {JSON.stringify(agentResult.payload, null, 2)}
              </pre>
            )}
            {agentResult.rejection_reason && (
              <p className="text-xs text-zinc-400">{agentResult.rejection_reason}</p>
            )}
          </div>
        )}
      </div>
    </Card>
  );
}
