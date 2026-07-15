import { describe, expect, it } from "vitest";

import { isWorkload, routeForKind, workloadKind } from "./workloads";

describe("workloadKind", () => {
  it("resolves the seven typed workload kinds", () => {
    expect(workloadKind({ group: "core", version: "v1", resource: "pods" })?.kind).toBe("Pod");
    expect(workloadKind({ group: "apps", version: "v1", resource: "deployments" })?.controller).toBe(true);
    expect(workloadKind({ group: "batch", version: "v1", resource: "cronjobs" })?.kind).toBe("CronJob");
  });

  it("returns undefined for the pod kind's controller flag being false", () => {
    expect(workloadKind({ group: "core", version: "v1", resource: "pods" })?.controller).toBe(false);
  });

  it("returns undefined for non-workload or mismatched refs", () => {
    expect(workloadKind({ group: "core", version: "v1", resource: "configmaps" })).toBeUndefined();
    // Right resource, wrong group — not a match.
    expect(workloadKind({ group: "core", version: "v1", resource: "deployments" })).toBeUndefined();
    expect(isWorkload({ group: "example.com", version: "v1", resource: "widgets" })).toBe(false);
  });
});

describe("routeForKind", () => {
  it("builds a detail route for a known controller kind", () => {
    expect(routeForKind("ReplicaSet", "default", "web-abc")).toBe(
      "/resources/apps/v1/replicasets/default/web-abc",
    );
    expect(routeForKind("Deployment", "prod", "api")).toBe("/resources/apps/v1/deployments/prod/api");
  });

  it("url-encodes namespace and name", () => {
    expect(routeForKind("Job", "team/a", "run 1")).toBe("/resources/batch/v1/jobs/team%2Fa/run%201");
  });

  it("returns undefined for kinds without a typed route", () => {
    expect(routeForKind("Node", "default", "n1")).toBeUndefined();
  });
});
