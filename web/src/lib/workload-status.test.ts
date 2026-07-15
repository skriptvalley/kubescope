import { describe, expect, it } from "vitest";

import { eventTypeTone, podStatusTone } from "./workload-status";

describe("podStatusTone", () => {
  it("marks healthy statuses ok", () => {
    expect(podStatusTone("Running")).toBe("ok");
    expect(podStatusTone("Completed")).toBe("ok");
  });

  it("marks crash/error statuses warn", () => {
    expect(podStatusTone("CrashLoopBackOff")).toBe("warn");
    expect(podStatusTone("Error")).toBe("warn");
    expect(podStatusTone("ImagePullBackOff")).toBe("warn");
    expect(podStatusTone("OOMKilled")).toBe("warn");
    expect(podStatusTone("Terminating")).toBe("warn");
  });

  it("marks init and transitional statuses progress", () => {
    expect(podStatusTone("Init:0/2")).toBe("progress");
    expect(podStatusTone("Pending")).toBe("progress");
    expect(podStatusTone("ContainerCreating")).toBe("progress");
  });
});

describe("eventTypeTone", () => {
  it("distinguishes Warning from Normal", () => {
    expect(eventTypeTone("Warning")).toBe("warn");
    expect(eventTypeTone("Normal")).toBe("neutral");
  });
});
