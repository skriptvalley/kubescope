import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { queryKeys } from "@/lib/query-keys";

/** The default depth the backend applies when the client sends none. Mirrored
 *  here only so the depth control opens on the value the server will use; the
 *  server remains the authority and echoes the depth it actually applied. */
export const DEFAULT_GRAPH_DEPTH = 3;

/** The relationship graph around one object (FB-14, ADR-0011). Every traversal,
 *  bound and aggregation is server-side; this is a plain read of the DTO.
 *  Disabled for cluster-scoped objects, which have no namespace to scope to. */
export function useResourceGraph(
  namespace: string | undefined,
  kind: string,
  name: string,
  depth: number,
) {
  return useQuery({
    queryKey: queryKeys.resourceGraph(namespace ?? "", kind, name, depth),
    queryFn: () => api.graph(namespace ?? "", { kind, name }, depth),
    enabled: Boolean(namespace && kind && name),
  });
}
