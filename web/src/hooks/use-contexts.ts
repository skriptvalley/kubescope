import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type ContextInfo } from "@/lib/api";
import { clusterScopedKeyPrefixes } from "@/lib/query-keys";

/** All kubeconfig contexts with their active flag. Cheap: no cluster calls. */
export function useContexts() {
  return useQuery({
    queryKey: ["contexts"],
    queryFn: api.contexts.list,
  });
}

/** Per-context connection health. Probes clusters, so it loads independently
 *  of the context list and refreshes periodically. */
export function useContextsHealth() {
  return useQuery({
    queryKey: ["contexts", "health"],
    queryFn: api.contexts.health,
    refetchInterval: 30_000,
  });
}

/** Switch the active context, then drop every cluster-scoped cache and refetch
 *  so all views (mounted or not) show the new cluster, never stale data. Live
 *  SSE streams re-subscribe automatically once their queries refetch. */
export function useSwitchContext() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.contexts.switch,
    onSuccess: (items: ContextInfo[]) => {
      // Seed the fresh context list so the switcher doesn't flash to loading.
      queryClient.setQueryData(["contexts"], items);
      // Refetch every mounted view (health + the current cluster page) so the
      // active page updates in place. Must run before removing anything: a
      // removed query has no observer to refetch, which would strand the current
      // page on the prior cluster's data until a manual refresh.
      void queryClient.invalidateQueries();
      // Drop only the *unmounted* cluster caches so a later navigation refetches
      // fresh instead of flashing the previous cluster's data. Active queries are
      // left for invalidateQueries above to refetch.
      for (const key of clusterScopedKeyPrefixes) {
        queryClient.removeQueries({ queryKey: key, type: "inactive" });
      }
    },
  });
}
