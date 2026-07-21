import { describe, expect, it } from "vitest";

import { eventTypeTone, podStatusTone, restartTone } from "./workload-status";

describe("podStatusTone", () => {
  it("marks running states ok (brand)", () => {
    expect(podStatusTone("Running")).toBe("ok");
    expect(podStatusTone("Ready")).toBe("ok");
  });

  it("marks terminal Completed/Succeeded neutral (muted)", () => {
    expect(podStatusTone("Completed")).toBe("neutral");
    expect(podStatusTone("Succeeded")).toBe("neutral");
  });

  it("marks crash/error/backoff statuses warn (destructive)", () => {
    expect(podStatusTone("CrashLoopBackOff")).toBe("warn");
    expect(podStatusTone("Error")).toBe("warn");
    expect(podStatusTone("Failed")).toBe("warn");
    expect(podStatusTone("Evicted")).toBe("warn");
    expect(podStatusTone("ImagePullBackOff")).toBe("warn");
    expect(podStatusTone("OOMKilled")).toBe("warn");
    expect(podStatusTone("CreateContainerConfigError")).toBe("warn");
  });

  it("marks init and transitional statuses progress (highlight)", () => {
    expect(podStatusTone("Init:0/2")).toBe("progress");
    expect(podStatusTone("Pending")).toBe("progress");
    expect(podStatusTone("ContainerCreating")).toBe("progress");
    expect(podStatusTone("Terminating")).toBe("progress");
  });

  it("falls back to neutral for unrecognized statuses", () => {
    expect(podStatusTone("SomethingElse")).toBe("neutral");
  });
});

describe("restartTone", () => {
  it("thresholds 0 / 1–5 / >5", () => {
    expect(restartTone(0)).toBe("neutral");
    expect(restartTone(1)).toBe("progress");
    expect(restartTone(5)).toBe("progress");
    expect(restartTone(6)).toBe("warn");
    expect(restartTone(17)).toBe("warn");
  });
});

describe("eventTypeTone", () => {
  it("distinguishes Warning from Normal", () => {
    expect(eventTypeTone("Warning")).toBe("warn");
    expect(eventTypeTone("Normal")).toBe("neutral");
  });
});
