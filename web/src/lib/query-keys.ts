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
] as const;
