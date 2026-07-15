import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, installFakeEventSource } from "@/test/fake-event-source";

import { usePodLogs } from "./use-pod-logs";

let restore: () => void;
beforeEach(() => {
  vi.useFakeTimers();
  restore = installFakeEventSource();
});
afterEach(() => {
  restore();
  vi.useRealTimers();
});

describe("usePodLogs", () => {
  it("clears buffered lines on reconnect so replayed logs do not duplicate", () => {
    const { result } = renderHook(() => usePodLogs("default", "web-1", { follow: true }));

    act(() => FakeEventSource.latest().emitOpen());
    act(() => FakeEventSource.latest().emitMessage(JSON.stringify({ line: "one" })));
    expect(result.current.lines).toEqual(["one"]);

    // Transient drop → backoff reconnect → the server replays; lines must reset.
    act(() => FakeEventSource.latest().emitError());
    act(() => vi.advanceTimersByTime(1000));
    act(() => FakeEventSource.latest().emitOpen());
    expect(result.current.lines).toEqual([]);

    act(() => FakeEventSource.latest().emitMessage(JSON.stringify({ line: "one" })));
    expect(result.current.lines).toEqual(["one"]);
  });

  it("surfaces a terminal closed event without reconnecting", () => {
    const { result } = renderHook(() => usePodLogs("default", "web-1", { follow: false }));
    act(() => FakeEventSource.latest().emitOpen());
    const before = FakeEventSource.instances.length;

    act(() => FakeEventSource.latest().emitNamed("closed", JSON.stringify({ reason: "eof" })));
    expect(result.current.closed).toEqual({ reason: "eof" });

    // No reconnect is scheduled after a terminal close.
    act(() => vi.advanceTimersByTime(60_000));
    expect(FakeEventSource.instances.length).toBe(before);
  });
});
