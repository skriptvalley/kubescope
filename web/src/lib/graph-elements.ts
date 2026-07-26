// DTO → Cytoscape mapping for the resource graph (FB-14, ADR-0011). Pure: no
// DOM, no cytoscape import — the view feeds these elements to the renderer, and
// this module is unit-tested on its own.
//
// The backend has already done every traversal, bound and aggregation. All that
// is left here is presentation: a label, a tone, a coarse category for shape and
// colour, and the detail route a click should follow.

import { groupToken, type GraphNode, type GraphRelation, type ResourceGraph } from "@/lib/api";
import { podStatusTone, type StatusTone } from "@/lib/workload-status";

/** Coarse buckets the stylesheet keys shape and colour off, so a graph reads
 *  by silhouette before it reads by label. CRDs land in "other". */
export type GraphCategory =
  | "workload"
  | "pod"
  | "network"
  | "config"
  | "storage"
  | "identity"
  | "other";

const CATEGORIES: Record<string, GraphCategory> = {
  Deployment: "workload",
  StatefulSet: "workload",
  DaemonSet: "workload",
  ReplicaSet: "workload",
  Job: "workload",
  CronJob: "workload",
  HorizontalPodAutoscaler: "workload",
  Pod: "pod",
  Service: "network",
  Ingress: "network",
  ConfigMap: "config",
  Secret: "config",
  PersistentVolumeClaim: "storage",
  PersistentVolume: "storage",
  ServiceAccount: "identity",
};

export function graphCategory(kind: string): GraphCategory {
  return CATEGORIES[kind] ?? "other";
}

/** A "ready/desired" status, which several controller kinds report. */
const RATIO = /^(\d+)\/(\d+)$/;

/** Worst-wins ordering, so a clubbed run series that contains one failure reads
 *  as a failure rather than averaging out to fine. */
const SEVERITY: Record<StatusTone, number> = { warn: 3, progress: 2, ok: 1, neutral: 0 };

/** Classifies a node's status into a tone. Ratios are decided here (nothing
 *  else in the app renders one); every other status goes through the app's one
 *  status→tone classifier (ADR-0009) rather than a second copy of the rule. */
export function graphStatusTone(status: string | undefined, missing = false): StatusTone {
  if (missing) return "warn";
  if (!status) return "neutral";
  const ratio = RATIO.exec(status);
  if (ratio) {
    const [ready, desired] = [Number(ratio[1]), Number(ratio[2])];
    if (desired > 0 && ready >= desired) return "ok";
    return ready === 0 ? "warn" : "progress";
  }
  return podStatusTone(status);
}

/** An aggregate's status is a tally ("3 Completed, 1 Failed"); its tone is the
 *  worst of the statuses inside, so clubbing never hides a failed run. */
export function aggregateTone(tally: string | undefined): StatusTone {
  if (!tally) return "neutral";
  let worst: StatusTone = "neutral";
  for (const part of tally.split(", ")) {
    const status = part.replace(/^\+?\d+\s+/, "");
    if (!status || status === "other") continue;
    const tone = graphStatusTone(status);
    if (SEVERITY[tone] > SEVERITY[worst]) worst = tone;
  }
  return worst;
}

/** The label under a node: an object's name, or "3 Jobs" for a clubbed set. */
export function nodeLabel(node: GraphNode): string {
  if (node.aggregate) return `${node.count ?? 0} ${node.kind}${node.count === 1 ? "" : "s"}`;
  return node.name;
}

/** Where clicking a node goes. A named object opens its detail; an aggregate
 *  stands for many objects, so it opens that kind's list rather than dead-ending. */
export function nodeRoute(node: GraphNode): string {
  const base = `/resources/${groupToken(node.group)}/${node.version}/${node.resource}`;
  if (node.aggregate) return base;
  if (node.namespace) return `${base}/${encodeURIComponent(node.namespace)}/${encodeURIComponent(node.name)}`;
  return `${base}/${encodeURIComponent(node.name)}`;
}

/** Labels that only restate their relation. "controller" sits on nearly every
 *  ownership edge and says nothing the arrow does not; "serviceAccountName" and
 *  "scaleTargetRef" repeat what the edge already is. They stay in the DTO — where
 *  a controlling owner is genuinely distinct from a plain one — but drawing them
 *  on every edge is noise the reader has to look past. */
const RESTATED_LABELS: Partial<Record<GraphRelation, string>> = {
  owns: "controller",
  serviceAccount: "serviceAccountName",
  scales: "scaleTargetRef",
};

/** The text drawn on an edge: its mechanism, unless that merely names the
 *  relation ("ready", "volume, envFrom" and "selector" all survive). */
export function edgeLabel(edge: { relation: GraphRelation; label?: string }): string {
  if (!edge.label || RESTATED_LABELS[edge.relation] === edge.label) return "";
  return edge.label;
}

/** One Cytoscape element. `data` is the renderer's data map; `classes` drives
 *  the stylesheet selectors. */
export interface GraphElement {
  data: Record<string, unknown>;
  classes: string;
}

/** Maps the graph DTO to Cytoscape elements: compound parents first (so a child
 *  never references a box that has not been declared), then nodes, then edges. */
export function toCytoscapeElements(graph: ResourceGraph): GraphElement[] {
  const routeByNode = new Map(graph.nodes.map((n) => [n.id, nodeRoute(n)]));

  const groups: GraphElement[] = graph.groups.map((group) => ({
    data: {
      id: group.id,
      label: group.label,
      kind: group.kind,
      // Clicking the box follows the workload it was built around.
      href: routeByNode.get(group.root) ?? "",
    },
    classes: "group",
  }));

  const nodes: GraphElement[] = graph.nodes.map((node) => {
    const tone = node.aggregate
      ? aggregateTone(node.status)
      : graphStatusTone(node.status, node.missing);
    const classes = [
      "node",
      `category-${graphCategory(node.kind)}`,
      `tone-${tone}`,
      node.focus ? "focus" : "",
      node.aggregate ? "aggregate" : "",
      node.missing ? "missing" : "",
    ].filter(Boolean);
    return {
      data: {
        id: node.id,
        parent: node.parent || undefined,
        label: nodeLabel(node),
        kind: node.kind,
        status: node.status ?? "",
        name: node.name,
        namespace: node.namespace ?? "",
        href: nodeRoute(node),
        tone,
      },
      classes: classes.join(" "),
    };
  });

  const edges: GraphElement[] = graph.edges.map((edge) => ({
    data: {
      id: edge.id,
      source: edge.source,
      target: edge.target,
      label: edgeLabel(edge),
      relation: edge.relation,
    },
    classes: `edge relation-${edge.relation}`,
  }));

  return [...groups, ...nodes, ...edges];
}
