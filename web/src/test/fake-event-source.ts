// A controllable EventSource stand-in for tests (jsdom has no EventSource).
// Tests install it on globalThis, then drive open/message/named/error events on
// the most recent instance.

type Handler = (event: { data?: string }) => void;

export class FakeEventSource {
  static instances: FakeEventSource[] = [];

  static reset() {
    FakeEventSource.instances = [];
  }

  static latest(): FakeEventSource {
    const es = FakeEventSource.instances.at(-1);
    if (!es) throw new Error("no FakeEventSource created yet");
    return es;
  }

  url: string;
  closed = false;
  onopen: Handler | null = null;
  onmessage: Handler | null = null;
  onerror: Handler | null = null;
  private listeners: Record<string, Handler[]> = {};

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, handler: Handler) {
    (this.listeners[type] ??= []).push(handler);
  }

  close() {
    this.closed = true;
  }

  // --- test drivers ---
  emitOpen() {
    this.onopen?.({});
  }
  emitMessage(data: string) {
    this.onmessage?.({ data });
  }
  emitNamed(type: string, data: string) {
    (this.listeners[type] ?? []).forEach((h) => h({ data }));
  }
  emitError() {
    this.onerror?.({});
  }
}

/** Installs FakeEventSource as the global EventSource, returning a restore fn. */
export function installFakeEventSource(): () => void {
  const original = globalThis.EventSource;
  FakeEventSource.reset();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).EventSource = FakeEventSource;
  return () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).EventSource = original;
  };
}
