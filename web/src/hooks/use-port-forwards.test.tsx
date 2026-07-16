import { afterEach, describe, expect, it } from "vitest";

import { connectivity } from "@/lib/connectivity";

import { portForwardsRefetchInterval } from "./use-port-forwards";

afterEach(() => connectivity.resetForTests());

describe("portForwardsRefetchInterval", () => {
  it("polls every 5s when reachable, backing off to 30s when unreachable", () => {
    expect(portForwardsRefetchInterval()).toBe(5_000);
    connectivity.setActiveUnreachable(true);
    expect(portForwardsRefetchInterval()).toBe(30_000);
  });
});
