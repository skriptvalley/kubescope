import { useEffect, useState } from "react";

import { podLogsUrl, type PodLogParams } from "@/lib/api";
import { openStream, type StreamStatus } from "@/lib/stream";

/** Cap on retained log lines so a chatty pod cannot grow the buffer unbounded;
 *  the oldest lines are dropped. */
const MAX_LINES = 5000;

export interface LogStreamState {
  lines: string[];
  status: StreamStatus;
  /** Set when the server ends the stream (container exit, pod deletion, or an
   *  upstream error) — a closed state, never a silent hang. */
  closed: { reason: string } | null;
}

/** Streams a pod's logs over SSE. Re-subscribes (and clears) whenever the pod,
 *  container, or options change. */
export function usePodLogs(namespace: string, name: string, params: PodLogParams): LogStreamState {
  const [lines, setLines] = useState<string[]>([]);
  const [status, setStatus] = useState<StreamStatus>("connecting");
  const [closed, setClosed] = useState<{ reason: string } | null>(null);

  const url = podLogsUrl(namespace, name, params);
  useEffect(() => {
    setLines([]);
    setClosed(null);
    setStatus("connecting");
    let stopped = false;

    const handle = openStream(url, {
      namedEvents: ["closed", "error"],
      onStatus: (next) => {
        if (!stopped) setStatus(next);
      },
      // On reconnect the server replays from the start (or tail); clear so the
      // replayed lines don't duplicate what we already showed.
      onReconnect: () => {
        if (!stopped) setLines([]);
      },
      onMessage: (data) => {
        if (stopped) return;
        let line: string | undefined;
        try {
          line = (JSON.parse(data) as { line?: string }).line;
        } catch {
          return;
        }
        if (line === undefined) return;
        setLines((prev) => {
          const next = prev.concat(line as string);
          return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
        });
      },
      onNamed: (event, data) => {
        if (stopped) return;
        stopped = true; // a terminal event ends the stream; no reconnect.
        setClosed({ reason: event === "closed" ? closeReason(data) : "error" });
        handle();
      },
    });

    return () => {
      stopped = true;
      handle();
    };
  }, [url]);

  return { lines, status, closed };
}

function closeReason(data: string): string {
  try {
    return (JSON.parse(data) as { reason?: string }).reason ?? "eof";
  } catch {
    return "eof";
  }
}
