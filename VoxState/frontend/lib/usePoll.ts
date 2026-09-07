"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "./api";

interface PollResult<T> {
  data: T | undefined;
  error: string | null;
  loading: boolean;
  refetch: () => void;
}

// usePoll fetches `fn` immediately, then again every `intervalMs`, and
// exposes a manual `refetch` for "do it right now" actions (e.g. right
// after the user submits a form). This is M8's "simplest appropriate
// mechanism" for keeping panels showing live backend state without
// introducing a websocket/SSE layer the brief explicitly didn't ask for.
//
// `key` is a single value (usually a machine/task id, or "" to mean "no
// target yet") identifying what `fn` fetches — a plain value rather than
// a dependency array, since a spread dependency array has an
// unpredictable length from React's point of view and trips
// react-hooks/exhaustive-deps.
export function usePoll<T>(
  fn: () => Promise<T>,
  key: string,
  intervalMs = 1000,
): PollResult<T> {
  const [data, setData] = useState<T>();
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // fnRef holds the latest `fn` closure without needing it in the
  // polling effect's own dependency array (callers typically pass a
  // fresh inline arrow function every render). Written from its own
  // effect, never during render, per react-hooks/refs.
  const fnRef = useRef(fn);
  useEffect(() => {
    fnRef.current = fn;
  });

  const run = useCallback(() => {
    let cancelled = false;
    fnRef
      .current()
      .then((result) => {
        if (cancelled) return;
        setData(result);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof ApiError ? err.message : "Unexpected error");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const [tick, setTick] = useState(0);

  useEffect(() => {
    const cancel = run();
    const id = setInterval(() => setTick((t) => t + 1), intervalMs);
    return () => {
      cancel();
      clearInterval(id);
    };
  }, [run, intervalMs, tick, key]);

  return { data, error, loading, refetch: () => setTick((t) => t + 1) };
}
