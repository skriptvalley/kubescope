import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, installFakeEventSource } from "@/test/fake-event-source";

import { openStream, type StreamStatus } from "./stream";

describe("openStream", () => {
  let restore: () => void;

  beforeEach(() => {
    vi.useFakeTimers();
    restore = installFakeEventSource();
  });
  afterEach(() => {
    restore();
    vi.useRealTimers();
  });

  it("goes live on open and forwards default messages", () => {
    const messages: string[] = [];
    const statuses: StreamStatus[] = [];
    openStream("/api/v1/stream/resources/core/v1/pods", {
      onMessage: (d) => messages.push(d),
      onStatus: (s) => statuses.push(s),
    });

    expect(statuses).toContain("connecting");
    FakeEventSource.latest().emitOpen();
    expect(statuses.at(-1)).toBe("live");

    FakeEventSource.latest().emitMessage('{"type":"add"}');
    expect(messages).toEqual(['{"type":"add"}']);
  });

  it("routes named events to onNamed", () => {
    const named: Array<[string, string]> = [];
    openStream("/api/v1/stream/pods/default/web/logs", {
      namedEvents: ["closed"],
      onNamed: (e, d) => named.push([e, d]),
    });
    FakeEventSource.latest().emitNamed("closed", '{"reason":"eof"}');
    expect(named).toEqual([["closed", '{"reason":"eof"}']]);
  });

  it("reconnects with exponential backoff after an error", () => {
    const statuses: StreamStatus[] = [];
    openStream("/url", { onStatus: (s) => statuses.push(s) });
    expect(FakeEventSource.instances).toHaveLength(1);

    // First drop → stale, reconnect after 1s.
    FakeEventSource.latest().emitError();
    expect(statuses.at(-1)).toBe("stale");
    vi.advanceTimersByTime(999);
    expect(FakeEventSource.instances).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(2);

    // Second drop → next backoff is 2s (not 1s).
    FakeEventSource.latest().emitError();
    vi.advanceTimersByTime(1999);
    expect(FakeEventSource.instances).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(FakeEventSource.instances).toHaveLength(3);
  });

  it("fires onReconnect only after a re-open, not the first open", () => {
    const reconnects: number[] = [];
    openStream("/url", { onReconnect: () => reconnects.push(1) });

    FakeEventSource.latest().emitOpen();
    expect(reconnects).toHaveLength(0);

    FakeEventSource.latest().emitError();
    vi.advanceTimersByTime(1000);
    FakeEventSource.latest().emitOpen();
    expect(reconnects).toHaveLength(1);
  });

  it("stops reconnecting once closed", () => {
    const close = openStream("/url", {});
    FakeEventSource.latest().emitError();
    close();
    vi.advanceTimersByTime(60_000);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("goes stale immediately when EventSource is unavailable (polling fallback)", () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).EventSource = undefined;
    const statuses: StreamStatus[] = [];
    openStream("/url", { onStatus: (s) => statuses.push(s) });
    expect(statuses.at(-1)).toBe("stale");
  });
});
