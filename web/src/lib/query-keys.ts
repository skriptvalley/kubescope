// Centralized TanStack Query keys. The live-update hooks (hooks/use-stream)
// patch the exact cache entries the base hooks populate, so both must key the
// same way — sharing these builders prevents drift that would silently break
// in-place cache patching.

import type { ResourceRef } from "@/lib/api";

export const queryKeys = {
  resourceList: (ref: Pick<ResourceRef, "group" | "version" | "resource" | "namespace">) =>
    ["resource-list", ref.group, ref.version, ref.resource, ref.namespace ?? ""] as const,
  resourceObject: (ref: ResourceRef) =>
    ["resource-get", ref.group, ref.version, ref.resource, ref.namespace ?? "", ref.name ?? ""] as const,
  resourceYaml: (ref: ResourceRef) =>
    ["resource-yaml", ref.group, ref.version, ref.resource, ref.namespace ?? "", ref.name ?? ""] as const,
  workloadSummary: (resource: string, namespace?: string) =>
    ["workload-summary", resource, namespace ?? ""] as const,
  eventsFeed: (namespace?: string) => ["events-feed", namespace ?? ""] as const,
  portForwards: () => ["port-forwards"] as const,
  serviceDetail: (namespace: string, name: string) =>
    ["service-detail", namespace, name] as const,
  resourceGraph: (namespace: string, kind: string, name: string, depth: number) =>
    ["resource-graph", namespace, kind, name, depth] as const,
  search: (query: string) => ["search", query] as const,
  // Registry-scoped (not cluster-scoped): the kubeconfig source registry is
  // shared across contexts, so it is NOT dropped on a context switch.
  kubeconfigs: () => ["kubeconfigs"] as const,
};

/** Query-key prefixes whose data is scoped to a single cluster; dropped on
 *  context switch (see useSwitchContext). */
export const clusterScopedKeyPrefixes = [
  ["overview"],
  ["nodes"],
  ["discovery"],
  ["namespaces"],
  ["resource-list"],
  ["resource-get"],
  ["resource-yaml"],
  ["workload-summary"],
  ["events-feed"],
  ["service-detail"],
  // The graph names objects in the active cluster (FB-14).
  ["resource-graph"],
  // Data enhancements (ADR-0009) — all per-cluster reads.
  ["counts"],
  ["metrics"],
  ["quotas"],
  // Search results are context-specific (they name objects in the active
  // cluster), so drop them on a context switch.
  ["search"],
  // Forwards are per-context and torn down server-side on a context switch, so
  // the list must refetch (the mounted panel) / drop (unmounted) on switch.
  ["port-forwards"],
] as const;
