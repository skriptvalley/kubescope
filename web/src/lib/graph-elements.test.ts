import { describe, expect, it } from "vitest";

import type { GraphNode, ResourceGraph } from "@/lib/api";
import {
  aggregateTone,
  edgeLabel,
  graphCategory,
  graphStatusTone,
  nodeLabel,
  nodeRoute,
  toCytoscapeElements,
} from "@/lib/graph-elements";

function node(overrides: Partial<GraphNode> & Pick<GraphNode, "id" | "kind" | "name">): GraphNode {
  return {
    group: "",
    version: "v1",
    resource: "pods",
    namespace: "web",
    depth: 1,
    ...overrides,
  };
}

/** The shape the backend returns for `focus=Deployment/api` in deploy/testenv's
 *  web namespace: a boxed workload plus the shared config outside the box. */
const GRAPH: ResourceGraph = {
  namespace: "web",
  focus: { group: "apps", version: "v1", resource: "deployments", kind: "Deployment", namespace: "web", name: "api" },
  depth: 3,
  nodes: [
    node({
      id: "apps/Deployment/web/api", group: "apps", resource: "deployments",
      kind: "Deployment", name: "api", status: "2/2", depth: 0, focus: true, parent: "group/apps/Deployment/web/api",
    }),
    node({
      id: "apps/ReplicaSet/web/api-7d9", group: "apps", resource: "replicasets",
      kind: "ReplicaSet", name: "api-7d9", status: "2/2", parent: "group/apps/Deployment/web/api",
    }),
    node({
      id: "core/Pod/web/api-7d9-aaa", resource: "pods", kind: "Pod", name: "api-7d9-aaa",
      status: "Running", depth: 2, parent: "group/apps/Deployment/web/api",
    }),
    node({
      id: "core/Service/web/api", resource: "services", kind: "Service", name: "api",
      status: "ClusterIP", depth: 3, parent: "group/apps/Deployment/web/api",
    }),
    node({
      id: "core/ConfigMap/web/frontend-config", resource: "configmaps",
      kind: "ConfigMap", name: "frontend-config", depth: 3,
    }),
  ],
  edges: [
    {
      id: "apps/Deployment/web/api->apps/ReplicaSet/web/api-7d9",
      source: "apps/Deployment/web/api", target: "apps/ReplicaSet/web/api-7d9",
      relation: "owns", label: "controller",
    },
    {
      id: "core/Service/web/api->core/Pod/web/api-7d9-aaa",
      source: "core/Service/web/api", target: "core/Pod/web/api-7d9-aaa",
      relation: "routes", label: "ready",
    },
    {
      id: "core/Pod/web/api-7d9-aaa->core/ConfigMap/web/frontend-config",
      source: "core/Pod/web/api-7d9-aaa", target: "core/ConfigMap/web/frontend-config",
      relation: "mounts", label: "volume, envFrom",
    },
  ],
  groups: [
    {
      id: "group/apps/Deployment/web/api", label: "api",
      kind: "Deployment", root: "apps/Deployment/web/api",
    },
  ],
  partial: false,
};

describe("toCytoscapeElements", () => {
  it("maps every node, edge and group", () => {
    const elements = toCytoscapeElements(GRAPH);
    expect(elements).toHaveLength(GRAPH.nodes.length + GRAPH.edges.length + GRAPH.groups.length);
  });

  it("declares compound parents before the nodes that reference them", () => {
    const elements = toCytoscapeElements(GRAPH);
    const groupIndex = elements.findIndex((e) => e.data.id === "group/apps/Deployment/web/api");
    const childIndex = elements.findIndex((e) => e.data.id === "core/Pod/web/api-7d9-aaa");
    expect(groupIndex).toBeGreaterThanOrEqual(0);
    expect(groupIndex).toBeLessThan(childIndex);
  });

  it("nests the workload's own nodes and leaves shared config outside", () => {
    const byId = new Map(toCytoscapeElements(GRAPH).map((e) => [e.data.id, e]));
    for (const id of [
      "apps/Deployment/web/api",
      "apps/ReplicaSet/web/api-7d9",
      "core/Pod/web/api-7d9-aaa",
      "core/Service/web/api",
    ]) {
      expect(byId.get(id)?.data.parent).toBe("group/apps/Deployment/web/api");
    }
    expect(byId.get("core/ConfigMap/web/frontend-config")?.data.parent).toBeUndefined();
  });

  it("marks the compound parent so the stylesheet can box it", () => {
    const group = toCytoscapeElements(GRAPH).find((e) => e.classes.includes("group"));
    expect(group?.data.label).toBe("api");
    // Clicking the box follows the workload it was built around.
    expect(group?.data.href).toBe("/resources/apps/v1/deployments/web/api");
  });

  it("classes each node by category, tone and role", () => {
    const byId = new Map(toCytoscapeElements(GRAPH).map((e) => [e.data.id, e]));
    expect(byId.get("apps/Deployment/web/api")?.classes).toContain("category-workload");
    expect(byId.get("apps/Deployment/web/api")?.classes).toContain("focus");
    expect(byId.get("apps/Deployment/web/api")?.classes).toContain("tone-ok");
    expect(byId.get("core/Pod/web/api-7d9-aaa")?.classes).toContain("category-pod");
    expect(byId.get("core/Service/web/api")?.classes).toContain("category-network");
    expect(byId.get("core/ConfigMap/web/frontend-config")?.classes).toContain("category-config");
  });

  it("classes each edge by relation so the view can style it", () => {
    const edges = toCytoscapeElements(GRAPH).filter((e) => e.classes.startsWith("edge"));
    expect(edges.map((e) => e.classes)).toEqual([
      "edge relation-owns",
      "edge relation-routes",
      "edge relation-mounts",
    ]);
    expect(edges[2].data.label).toBe("volume, envFrom");
    // "controller" is on nearly every ownership edge and adds nothing visually.
    expect(edges[0].data.label).toBe("");
  });

  it("labels a clubbed set by its count and routes it to the kind's list", () => {
    const clubbed: ResourceGraph = {
      ...GRAPH,
      nodes: [
        node({
          id: "aggregate/batch/CronJob/batch/hourly-report/Job",
          group: "batch", resource: "jobs", kind: "Job", name: "",
          namespace: "batch", aggregate: true, count: 3, status: "3 Completed",
        }),
      ],
      edges: [],
      groups: [],
    };
    const [element] = toCytoscapeElements(clubbed);
    expect(element.data.label).toBe("3 Jobs");
    expect(element.classes).toContain("aggregate");
    expect(element.data.href).toBe("/resources/batch/v1/jobs");
  });

  it("marks a dangling reference as missing and failing", () => {
    const dangling: ResourceGraph = {
      ...GRAPH,
      nodes: [node({ id: "core/Secret/web/gone", resource: "secrets", kind: "Secret", name: "gone", missing: true, status: "Missing" })],
      edges: [],
      groups: [],
    };
    const [element] = toCytoscapeElements(dangling);
    expect(element.classes).toContain("missing");
    expect(element.classes).toContain("tone-warn");
  });
});

describe("edgeLabel", () => {
  it("drops labels that only restate the relation", () => {
    expect(edgeLabel({ relation: "owns", label: "controller" })).toBe("");
    expect(edgeLabel({ relation: "serviceAccount", label: "serviceAccountName" })).toBe("");
    expect(edgeLabel({ relation: "scales", label: "scaleTargetRef" })).toBe("");
    expect(edgeLabel({ relation: "owns" })).toBe("");
  });

  it("keeps every label that carries information", () => {
    expect(edgeLabel({ relation: "owns", label: "runs" })).toBe("runs");
    expect(edgeLabel({ relation: "routes", label: "ready" })).toBe("ready");
    expect(edgeLabel({ relation: "routes", label: "not ready" })).toBe("not ready");
    expect(edgeLabel({ relation: "routes", label: "selector" })).toBe("selector");
    expect(edgeLabel({ relation: "mounts", label: "volume, envFrom" })).toBe("volume, envFrom");
    expect(edgeLabel({ relation: "claims", label: "bound" })).toBe("bound");
    // The same word on a different relation is not the restatement.
    expect(edgeLabel({ relation: "mounts", label: "controller" })).toBe("controller");
  });
});

describe("graphStatusTone", () => {
  it.each([
    ["", "neutral"],
    ["Running", "ok"],
    ["CrashLoopBackOff", "warn"],
    ["Pending", "progress"],
    ["Completed", "neutral"],
    ["Bound", "neutral"],
    ["ClusterIP", "neutral"],
    ["*/1 * * * *", "neutral"],
    ["3/3", "ok"],
    ["2/3", "progress"],
    ["0/3", "warn"],
    ["0/0", "warn"],
  ])("classifies %s as %s", (status, want) => {
    expect(graphStatusTone(status)).toBe(want);
  });

  it("treats a missing object as failing whatever its status says", () => {
    expect(graphStatusTone("", true)).toBe("warn");
    expect(graphStatusTone("Running", true)).toBe("warn");
  });
});

describe("aggregateTone", () => {
  it.each([
    [undefined, "neutral"],
    ["3 Completed", "neutral"],
    ["2 Running", "ok"],
    ["3 Completed, 1 Failed", "warn"],
    ["2 Completed, 1 Pending", "progress"],
    ["2 Running, 1 Completed, 1 Failed, +2 other", "warn"],
  ])("takes the worst tone in %s", (tally, want) => {
    expect(aggregateTone(tally)).toBe(want);
  });
});

describe("nodeLabel and nodeRoute", () => {
  it("labels a single clubbed object without pluralizing", () => {
    expect(nodeLabel(node({ id: "a", kind: "Job", name: "", aggregate: true, count: 1 }))).toBe("1 Job");
  });

  it("routes a namespaced object to its detail view", () => {
    expect(nodeRoute(node({ id: "a", kind: "Pod", name: "api-1" }))).toBe("/resources/core/v1/pods/web/api-1");
  });

  it("routes a cluster-scoped object without a namespace segment", () => {
    expect(
      nodeRoute(node({ id: "a", kind: "PersistentVolume", name: "pvc-9", resource: "persistentvolumes", namespace: undefined })),
    ).toBe("/resources/core/v1/persistentvolumes/pvc-9");
  });

  it("escapes names that are not URL-safe", () => {
    expect(nodeRoute(node({ id: "a", kind: "Pod", name: "a b/c" }))).toContain("a%20b%2Fc");
  });
});

describe("graphCategory", () => {
  it("buckets known kinds and falls back for CRDs", () => {
    expect(graphCategory("Deployment")).toBe("workload");
    expect(graphCategory("Pod")).toBe("pod");
    expect(graphCategory("Ingress")).toBe("network");
    expect(graphCategory("Secret")).toBe("config");
    expect(graphCategory("PersistentVolumeClaim")).toBe("storage");
    expect(graphCategory("ServiceAccount")).toBe("identity");
    expect(graphCategory("Rollout")).toBe("other");
  });
});
