import { afterEach, describe, expect, it, vi } from "vitest";

import { connectivity } from "./connectivity";

afterEach(() => connectivity.resetForTests());

describe("connectivity store", () => {
  it("defaults to reachable and never-connected", () => {
    expect(connectivity.isActiveUnreachable()).toBe(false);
    expect(connectivity.hasEverConnected()).toBe(false);
  });

  it("notifies subscribers only when the unreachable flag actually changes", () => {
    const listener = vi.fn();
    const unsubscribe = connectivity.subscribe(listener);

    connectivity.setActiveUnreachable(true);
    expect(connectivity.isActiveUnreachable()).toBe(true);
    expect(listener).toHaveBeenCalledTimes(1);

    // Setting the same value again is a no-op — no spurious notification.
    connectivity.setActiveUnreachable(true);
    expect(listener).toHaveBeenCalledTimes(1);

    connectivity.setActiveUnreachable(false);
    expect(listener).toHaveBeenCalledTimes(2);

    unsubscribe();
    connectivity.setActiveUnreachable(true);
    expect(listener).toHaveBeenCalledTimes(2);
  });

  it("latches everConnected once and notifies exactly once", () => {
    const listener = vi.fn();
    connectivity.subscribe(listener);

    connectivity.markEverConnected();
    expect(connectivity.hasEverConnected()).toBe(true);
    expect(listener).toHaveBeenCalledTimes(1);

    // Idempotent: a second mark does not fire listeners again.
    connectivity.markEverConnected();
    expect(listener).toHaveBeenCalledTimes(1);
  });
});
