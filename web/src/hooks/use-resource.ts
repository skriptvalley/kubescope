import { useQuery } from "@tanstack/react-query";

import { api, type ResourceRef } from "@/lib/api";
import { queryKeys } from "@/lib/query-keys";

/** A generic resource list for one GVR + namespace scope. `refetchInterval`
 *  lets live callers enable polling only while the SSE stream is unavailable. */
export function useResourceList(ref: ResourceRef, refetchInterval: number | false = false) {
  return useQuery({
    queryKey: queryKeys.resourceList(ref),
    queryFn: () => api.resources.list(ref),
    refetchInterval,
  });
}

/** A single object's full body (for metadata rendering). `enabled` lets callers
 *  skip the fetch when the object body is not rendered (e.g. controller detail
 *  views resolve their own data and only the YAML tab — a separate query —
 *  needs the raw object). */
export function useResourceObject(ref: ResourceRef, enabled = true) {
  return useQuery({
    queryKey: queryKeys.resourceObject(ref),
    queryFn: () => api.resources.get(ref),
    enabled,
  });
}

/** A single object rendered as YAML. Fetched lazily (enabled) so it only loads
 *  when the YAML tab is opened. */
export function useResourceYaml(ref: ResourceRef, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.resourceYaml(ref),
    queryFn: () => api.resources.yaml(ref),
    enabled,
  });
}
