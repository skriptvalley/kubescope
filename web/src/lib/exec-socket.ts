// Low-level exec WebSocket client (ADR-0006). The exec terminal is genuinely
// bidirectional — stdin up, stdout down, plus resize control — so it rides a
// WebSocket rather than SSE. Wire protocol mirrors internal/stream/exec.go:
//   - binary frames carry raw terminal bytes (stdin up, stdout down);
//   - text frames carry a JSON control message ({type:"resize"} up;
//     {type:"exit"|"error"} down, sent just before the server closes).
// Unlike the SSE client there is no auto-reconnect: an exec session is one-shot,
// and reconnection is an explicit user action (a fresh session).

export type ExecStatus = "connecting" | "open" | "closed";

/** A terminal end: a clean/coded process exit, or a structured error. */
export interface ExecExit {
  reason: string;
}

export interface ExecCallbacks {
  /** Remote stdout/stderr bytes (TTY-merged). */
  onData?: (data: Uint8Array) => void;
  /** Connection state transitions. */
  onStatus?: (status: ExecStatus) => void;
  /** The session ended (process exit or error), before the socket closes. */
  onExit?: (exit: ExecExit) => void;
}

/** Handle on an open exec session. */
export interface ExecSocketHandle {
  /** Send stdin (as a binary frame). */
  send: (data: string) => void;
  /** Send a terminal-resize control frame. */
  resize: (cols: number, rows: number) => void;
  /** Close the session permanently. */
  close: () => void;
}

interface ControlMessage {
  type: "resize" | "exit" | "error";
  cols?: number;
  rows?: number;
  code?: number;
  message?: string;
}

const encoder = new TextEncoder();

/** Opens an exec WebSocket to a relative /api path. Returns a handle; the caller
 *  closes it to end the session. */
export function openExecSocket(url: string, callbacks: ExecCallbacks): ExecSocketHandle {
  let closed = false;
  const WebSocketImpl = globalThis.WebSocket;
  if (!WebSocketImpl) {
    // No WebSocket (headless/unsupported): report closed so the UI offers retry.
    callbacks.onStatus?.("closed");
    return { send: () => {}, resize: () => {}, close: () => {} };
  }

  callbacks.onStatus?.("connecting");
  const ws = new WebSocketImpl(toWebSocketUrl(url));
  ws.binaryType = "arraybuffer";

  ws.onopen = () => {
    if (!closed) callbacks.onStatus?.("open");
  };
  ws.onmessage = (event: MessageEvent) => {
    if (closed) return;
    if (typeof event.data === "string") {
      handleControl(event.data, callbacks);
      return;
    }
    callbacks.onData?.(new Uint8Array(event.data as ArrayBuffer));
  };
  ws.onclose = () => {
    if (closed) return;
    closed = true;
    callbacks.onStatus?.("closed");
  };
  // onerror is followed by onclose; the close handler reports the terminal state.
  ws.onerror = () => {};

  const isOpen = () => ws.readyState === WebSocketImpl.OPEN;

  return {
    // stdin must be a binary frame: a text frame would be parsed as a control
    // message by the server. Encode the keystrokes/paste to bytes.
    send: (data: string) => {
      if (isOpen()) ws.send(encoder.encode(data));
    },
    resize: (cols: number, rows: number) => {
      if (isOpen()) ws.send(JSON.stringify({ type: "resize", cols, rows } satisfies ControlMessage));
    },
    close: () => {
      closed = true;
      ws.close();
    },
  };
}

function handleControl(raw: string, callbacks: ExecCallbacks) {
  let msg: ControlMessage;
  try {
    msg = JSON.parse(raw) as ControlMessage;
  } catch {
    return;
  }
  if (msg.type === "exit") {
    callbacks.onExit?.({ reason: exitReason(msg.code) });
  } else if (msg.type === "error") {
    callbacks.onExit?.({ reason: msg.message ?? "session error" });
  }
}

function exitReason(code?: number): string {
  if (code === undefined || code === 0) return "process exited";
  return `process exited (code ${code})`;
}

/** Resolves a relative /api path to an absolute ws(s):// URL for the origin. */
function toWebSocketUrl(path: string): string {
  const loc = globalThis.location;
  const protocol = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${loc.host}${path}`;
}
