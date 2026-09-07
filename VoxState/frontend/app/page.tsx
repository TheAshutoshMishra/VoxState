"use client";

import { useState } from "react";
import { BackendStatus } from "@/components/BackendStatus";
import { MachineSelector } from "@/components/MachineSelector";
import { MachinePanel } from "@/components/MachinePanel";
import { StateTimeline } from "@/components/StateTimeline";
import { TaskPanel } from "@/components/TaskPanel";
import { PolicyPanel } from "@/components/PolicyPanel";
import { VoicePanel } from "@/components/VoicePanel";
import { ActivityStream } from "@/components/ActivityStream";

export default function Home() {
  const [machineId, setMachineId] = useState("");

  return (
    <div className="mx-auto flex w-full max-w-[1600px] flex-1 flex-col gap-4 p-4 sm:p-6">
      <header className="flex flex-col gap-3 border-b border-zinc-800 pb-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-xl font-bold tracking-tight">
            VoxState <span className="font-normal text-zinc-500">control surface</span>
          </h1>
          <BackendStatus />
        </div>
        <MachineSelector selectedId={machineId} onSelect={setMachineId} />
      </header>

      <main className="grid flex-1 grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="flex flex-col gap-4">
          <MachinePanel machineId={machineId} />
          <StateTimeline machineId={machineId} />
        </div>
        <div className="flex flex-col gap-4">
          <TaskPanel machineId={machineId} />
          <PolicyPanel machineId={machineId} />
        </div>
        <div className="flex flex-col gap-4">
          <VoicePanel machineId={machineId} />
          <ActivityStream />
        </div>
      </main>

      <footer className="border-t border-zinc-800 pt-3 text-center text-xs text-zinc-600">
        VoxState M8 — every value on this page comes from the live Go
        backend; this UI never decides ACCEPTED/REJECTED/STALE itself.
      </footer>
    </div>
  );
}
