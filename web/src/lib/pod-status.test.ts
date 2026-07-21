import { describe, expect, it } from "vitest";

import { podDisplayStatus } from "./pod-status";

describe("podDisplayStatus", () => {
  it("returns the phase for a healthy running pod", () => {
    expect(podDisplayStatus({ status: { phase: "Running", containerStatuses: [{ ready: true }] } })).toBe(
      "Running",
    );
  });

  it("surfaces a crash waiting reason over the phase", () => {
    expect(
      podDisplayStatus({
        status: {
          phase: "Running",
          containerStatuses: [{ state: { waiting: { reason: "CrashLoopBackOff" } } }],
        },
      }),
    ).toBe("CrashLoopBackOff");
  });

  it("ignores transient waiting reasons and falls back to phase", () => {
    expect(
      podDisplayStatus({
        status: {
          phase: "Pending",
          containerStatuses: [{ state: { waiting: { reason: "ContainerCreating" } } }],
        },
      }),
    ).toBe("ContainerCreating");
  });

  it("reads Terminating from a deletion timestamp on a non-terminal pod", () => {
    expect(
      podDisplayStatus({
        metadata: { deletionTimestamp: "2026-07-21T00:00:00Z" },
        status: { phase: "Running" },
      }),
    ).toBe("Terminating");
  });

  it("keeps a terminal phase during deletion", () => {
    expect(
      podDisplayStatus({
        metadata: { deletionTimestamp: "2026-07-21T00:00:00Z" },
        status: { phase: "Succeeded" },
      }),
    ).toBe("Succeeded");
  });

  it("prefers a pod-level reason like Evicted", () => {
    expect(podDisplayStatus({ status: { phase: "Failed", reason: "Evicted" } })).toBe("Evicted");
  });
});
