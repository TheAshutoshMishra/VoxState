"use client";

// A tiny pub/sub so any panel can report an event it already received
// inline in an HTTP response (state changes, task create/cancel, policy
// decisions — see internal/api's *Response DTOs, each of which already
// embeds the events.Event(s) that request produced) into the same
// unified activity feed the backend-polled /activity entries render
// into. This is intentionally not Redux/Zustand/etc — one Context, one
// array, no unnecessary abstraction for what is a single shared list.
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";

export interface LocalEvent {
  message: string;
  time: string;
  attrs?: Record<string, unknown>;
}

interface ActivityLogContextValue {
  localEvents: LocalEvent[];
  push: (event: LocalEvent) => void;
}

const ActivityLogContext = createContext<ActivityLogContextValue | null>(
  null,
);

const MAX_LOCAL_EVENTS = 100;

export function ActivityLogProvider({ children }: { children: ReactNode }) {
  const [localEvents, setLocalEvents] = useState<LocalEvent[]>([]);

  const push = useCallback((event: LocalEvent) => {
    setLocalEvents((prev) => [...prev, event].slice(-MAX_LOCAL_EVENTS));
  }, []);

  const value = useMemo(() => ({ localEvents, push }), [localEvents, push]);

  return (
    <ActivityLogContext.Provider value={value}>
      {children}
    </ActivityLogContext.Provider>
  );
}

export function useActivityLog(): ActivityLogContextValue {
  const ctx = useContext(ActivityLogContext);
  if (!ctx) {
    throw new Error("useActivityLog must be used within ActivityLogProvider");
  }
  return ctx;
}
