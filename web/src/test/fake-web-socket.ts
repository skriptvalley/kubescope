// A controllable WebSocket stand-in for tests (jsdom has no WebSocket). Tests
// install it on globalThis, then drive open/message/close on the latest instance
// and inspect what the client sent. Mirrors test/fake-event-source.ts.

type Handler = (event: { data?: unknown }) => void;

export class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  static instances: FakeWebSocket[] = [];

  static reset() {
    FakeWebSocket.instances = [];
  }

  static latest(): FakeWebSocket {
    const ws = FakeWebSocket.instances.at(-1);
    if (!ws) throw new Error("no FakeWebSocket created yet");
    return ws;
  }

  url: string;
  binaryType = "blob";
  readyState = FakeWebSocket.CONNECTING;
  onopen: Handler | null = null;
  onmessage: Handler | null = null;
  onclose: Handler | null = null;
  onerror: Handler | null = null;

  /** Everything the client sent, in order (binary as Uint8Array, control as string). */
  sent: Array<string | Uint8Array> = [];

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(data: string | ArrayBufferView | ArrayBuffer) {
    if (typeof data === "string") {
      this.sent.push(data);
    } else if (ArrayBuffer.isView(data)) {
      this.sent.push(new Uint8Array(data.buffer, data.byteOffset, data.byteLength));
    } else {
      this.sent.push(new Uint8Array(data));
    }
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({});
  }

  // --- test drivers ---
  emitOpen() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.({});
  }
  /** Deliver a binary (stdout) frame. */
  emitBinary(bytes: Uint8Array) {
    this.onmessage?.({ data: bytes.buffer });
  }
  /** Deliver a text control frame. */
  emitControl(msg: unknown) {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  emitClose() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({});
  }
}

/** Installs FakeWebSocket as the global WebSocket, returning a restore fn. */
export function installFakeWebSocket(): () => void {
  const original = globalThis.WebSocket;
  FakeWebSocket.reset();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).WebSocket = FakeWebSocket;
  return () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).WebSocket = original;
  };
}
