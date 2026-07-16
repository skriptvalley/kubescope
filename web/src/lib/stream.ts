// Low-level Server-Sent Events client with reconnect + backoff (ADR-0006). The
// browser's EventSource auto-reconnects, but we drive reconnection ourselves so
// backoff is deterministic and the connection state (live/stale) is observable
// for the live/stale indicator and the polling fallback. Higher-level hooks in
// hooks/use-stream wrap this to patch the TanStack Query cache.

export type StreamStatus = "connecting" | "live" | "stale";

/** Connectivity transition carried by a "status" watch event (FB-6 Story D).
 *  The backend emits one on reachable⇄unreachable transitions per stream. */
export interface StatusInfo {
  state: "unreachable" | "connected";
  reason?: string;
  message?: string;
  guidance?: string;
}

/** One watch notification off the resource stream (mirrors internal/stream). */
export interface WatchEvent {
  type: "add" | "update" | "delete" | "resync" | "status";
  row?: unknown;
  object?: Record<string, unknown>;
  ref?: { name: string; namespace?: string; uid?: string };
  status?: StatusInfo;
}

export interface StreamCallbacks {
  /** Unnamed (default) SSE `data:` frames — watch events, log lines. */
  onMessage?: (data: string) => void;
  /** Named SSE events (e.g. log `closed`/`error`). Subscribe via namedEvents. */
  onNamed?: (event: string, data: string) => void;
  /** Connection state transitions. */
  onStatus?: (status: StreamStatus) => void;
  /** Fires when a connection re-opens after a drop (not on first open): the
   *  consumer should refetch a clean baseline to cover the gap. */
  onReconnect?: () => void;
  /** Named event types to subscribe to (EventSource needs explicit listeners). */
  namedEvents?: string[];
}

const BASE_BACKOFF_MS = 1000;
const MAX_BACKOFF_MS = 30000;

/** Opens a reconnecting SSE stream. Returns a close function that permanently
 *  stops the stream (no further reconnects). When EventSource is unavailable,
 *  the status goes straight to "stale" so callers fall back to polling. */
export function openStream(url: string, callbacks: StreamCallbacks): () => void {
  let source: EventSource | null = null;
  let closed = false;
  let attempt = 0;
  let hadOpen = false;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;

  const setStatus = (status: StreamStatus) => callbacks.onStatus?.(status);

  const scheduleReconnect = () => {
    if (closed) return;
    const delay = Math.min(MAX_BACKOFF_MS, BASE_BACKOFF_MS * 2 ** attempt);
    attempt += 1;
    reconnectTimer = setTimeout(connect, delay);
  };

  function connect() {
    if (closed) return;
    const EventSourceImpl = globalThis.EventSource;
    if (!EventSourceImpl) {
      // No SSE support (or a headless environment): callers poll instead.
      setStatus("stale");
      return;
    }

    setStatus("connecting");
    const es = new EventSourceImpl(url);
    source = es;

    es.onopen = () => {
      attempt = 0;
      setStatus("live");
      if (hadOpen) callbacks.onReconnect?.();
      hadOpen = true;
    };
    es.onmessage = (event: MessageEvent) => callbacks.onMessage?.(event.data);
    for (const name of callbacks.namedEvents ?? []) {
      es.addEventListener(name, (event) => callbacks.onNamed?.(name, (event as MessageEvent).data));
    }
    es.onerror = () => {
      // Take control of reconnection: close and back off ourselves rather than
      // let EventSource retry on its own opaque schedule.
      setStatus("stale");
      es.close();
      if (source === es) source = null;
      scheduleReconnect();
    };
  }

  connect();

  return () => {
    closed = true;
    if (reconnectTimer !== undefined) clearTimeout(reconnectTimer);
    source?.close();
    source = null;
  };
}
