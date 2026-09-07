"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { useActivityLog } from "@/lib/activityLog";
import { usePoll } from "@/lib/usePoll";
import { Button, Label, TextInput } from "./Field";
import { Select } from "./Field";
import { ErrorBanner } from "./ErrorBanner";

export function MachineSelector({
  selectedId,
  onSelect,
}: {
  selectedId: string;
  onSelect: (id: string) => void;
}) {
  const { data: machines, error, loading, refetch } = usePoll(
    () => api.listMachines(),
    "machines",
    5000,
  );
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [id, setId] = useState("");
  const [type, setType] = useState("");
  const [location, setLocation] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const { push } = useActivityLog();

  // Auto-select the first machine once the list loads, so the dashboard
  // is never stuck on an empty "no machine selected" state if at least
  // one machine already exists.
  useEffect(() => {
    if (!selectedId && machines && machines.length > 0) {
      onSelect(machines[0].id);
    }
  }, [selectedId, machines, onSelect]);

  async function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;
    setSubmitting(true);
    setCreateError(null);
    try {
      const res = await api.createMachine({
        id: id.trim() || undefined,
        name: name.trim(),
        type: type.trim() || undefined,
        location: location.trim() || undefined,
      });
      push({
        message: res.event.type,
        time: res.event.created_at,
        attrs: { machine_id: res.machine.id },
      });
      setName("");
      setId("");
      setType("");
      setLocation("");
      setShowCreate(false);
      onSelect(res.machine.id);
      refetch();
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : "Failed to create machine");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-3">
      <Label htmlFor="machine-select">Machine</Label>
      {loading && !machines ? (
        <span className="text-sm text-zinc-500">Loading machines…</span>
      ) : (
        <Select
          id="machine-select"
          value={selectedId}
          onChange={(e) => onSelect(e.target.value)}
        >
          {(!machines || machines.length === 0) && (
            <option value="">No machines yet</option>
          )}
          {machines?.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name} ({m.id})
            </option>
          ))}
        </Select>
      )}
      <Button variant="secondary" onClick={() => setShowCreate((v) => !v)}>
        {showCreate ? "Cancel" : "+ New Machine"}
      </Button>
      {error && <ErrorBanner message={error} onRetry={refetch} />}

      {showCreate && (
        <form
          onSubmit={handleCreate}
          className="flex w-full flex-wrap items-end gap-2 rounded border border-zinc-800 bg-zinc-900/50 p-3"
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-machine-name">Name *</Label>
            <TextInput
              id="new-machine-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Machine 17"
              required
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-machine-id">ID (optional)</Label>
            <TextInput
              id="new-machine-id"
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder="machine-17"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-machine-type">Type</Label>
            <TextInput
              id="new-machine-type"
              value={type}
              onChange={(e) => setType(e.target.value)}
              placeholder="CNC mill"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-machine-location">Location</Label>
            <TextInput
              id="new-machine-location"
              value={location}
              onChange={(e) => setLocation(e.target.value)}
              placeholder="Floor 2"
            />
          </div>
          <Button type="submit" variant="primary" disabled={submitting}>
            {submitting ? "Creating…" : "Create"}
          </Button>
          {createError && <ErrorBanner message={createError} />}
        </form>
      )}
    </div>
  );
}
