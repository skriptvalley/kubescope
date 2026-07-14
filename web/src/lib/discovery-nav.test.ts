import { describe, expect, it } from "vitest";

import type { Discovery } from "./api";
import { buildNav, findResource } from "./discovery-nav";

const discovery: Discovery = {
  groups: [
    {
      name: "",
      resources: [
        { group: "", version: "v1", resource: "pods", kind: "Pod", namespaced: true, verbs: ["list"] },
        { group: "", version: "v1", resource: "nodes", kind: "Node", namespaced: false, verbs: ["list"] },
      ],
    },
    {
      name: "apps",
      resources: [
        {
          group: "apps",
          version: "v1",
          resource: "deployments",
          kind: "Deployment",
          namespaced: true,
          verbs: ["list"],
        },
      ],
    },
  ],
};

describe("buildNav", () => {
  it("labels the core group and routes it through the core token", () => {
    const nav = buildNav(discovery);
    expect(nav[0].label).toBe("core");
    expect(nav[0].resources[0].label).toBe("Pod");
    expect(nav[0].resources[0].to).toBe("/resources/core/v1/pods");
    expect(nav[1].label).toBe("apps");
    expect(nav[1].resources[0].to).toBe("/resources/apps/v1/deployments");
  });

  it("returns an empty nav for undefined discovery", () => {
    expect(buildNav(undefined)).toEqual([]);
  });
});

describe("findResource", () => {
  it("resolves scope and kind by URL token / version / resource", () => {
    expect(findResource(discovery, { group: "core", version: "v1", resource: "nodes" })?.namespaced).toBe(false);
    expect(findResource(discovery, { group: "apps", version: "v1", resource: "deployments" })?.kind).toBe(
      "Deployment",
    );
  });

  it("returns undefined for an unknown resource", () => {
    expect(findResource(discovery, { group: "core", version: "v1", resource: "ghost" })).toBeUndefined();
  });
});
