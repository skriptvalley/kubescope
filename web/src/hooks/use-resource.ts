import { useQuery } from "@tanstack/react-query";

import { api, type ResourceRef } from "@/lib/api";

/** A generic resource list for one GVR + namespace scope. */
export function useResourceList(ref: ResourceRef) {
  return useQuery({
    queryKey: ["resource-list", ref.group, ref.version, ref.resource, ref.namespace ?? ""],
    queryFn: () => api.resources.list(ref),
  });
}

/** A single object's full body (for metadata rendering). `enabled` lets callers
 *  skip the fetch when the object body is not rendered (e.g. controller detail
 *  views resolve their own data and only the YAML tab — a separate query —
 *  needs the raw object). */
export function useResourceObject(ref: ResourceRef, enabled = true) {
  return useQuery({
    queryKey: ["resource-get", ref.group, ref.version, ref.resource, ref.namespace ?? "", ref.name ?? ""],
    queryFn: () => api.resources.get(ref),
    enabled,
  });
}

/** A single object rendered as YAML. Fetched lazily (enabled) so it only loads
 *  when the YAML tab is opened. */
export function useResourceYaml(ref: ResourceRef, enabled: boolean) {
  return useQuery({
    queryKey: ["resource-yaml", ref.group, ref.version, ref.resource, ref.namespace ?? "", ref.name ?? ""],
    queryFn: () => api.resources.yaml(ref),
    enabled,
  });
}
