"use client";

import { useState } from "react";
import { Button, TextInput } from "./Field";

export interface KeyValuePair {
  key: string;
  value: string;
}

// A small, generic attribute editor (add/remove key=value rows) used by
// both machine creation and state-change forms — MachineState.Attributes
// is a free-form map on the backend (internal/state/machine.go), so the
// UI doesn't hard-code which attributes exist.
export function KeyValueEditor({
  pairs,
  onChange,
  label = "Attributes",
}: {
  pairs: KeyValuePair[];
  onChange: (pairs: KeyValuePair[]) => void;
  label?: string;
}) {
  const [draftKey, setDraftKey] = useState("");
  const [draftValue, setDraftValue] = useState("");

  function addPair() {
    const key = draftKey.trim();
    if (!key) return;
    onChange([...pairs.filter((p) => p.key !== key), { key, value: draftValue }]);
    setDraftKey("");
    setDraftValue("");
  }

  function removePair(key: string) {
    onChange(pairs.filter((p) => p.key !== key));
  }

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-medium text-zinc-400">{label}</span>
      {pairs.length > 0 && (
        <ul className="flex flex-col gap-1">
          {pairs.map((p) => (
            <li
              key={p.key}
              className="flex items-center justify-between gap-2 rounded border border-zinc-800 bg-zinc-900 px-2 py-1 text-xs"
            >
              <span>
                <span className="text-zinc-400">{p.key}:</span>{" "}
                <span className="text-zinc-100">{p.value}</span>
              </span>
              <button
                type="button"
                aria-label={`Remove attribute ${p.key}`}
                onClick={() => removePair(p.key)}
                className="text-zinc-500 hover:text-red-400"
              >
                ✕
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="flex items-center gap-1.5">
        <TextInput
          aria-label="Attribute name"
          placeholder="key (e.g. temperature)"
          value={draftKey}
          onChange={(e) => setDraftKey(e.target.value)}
          className="flex-1"
        />
        <TextInput
          aria-label="Attribute value"
          placeholder="value (e.g. 82)"
          value={draftValue}
          onChange={(e) => setDraftValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              addPair();
            }
          }}
          className="flex-1"
        />
        <Button type="button" onClick={addPair}>
          Add
        </Button>
      </div>
    </div>
  );
}
