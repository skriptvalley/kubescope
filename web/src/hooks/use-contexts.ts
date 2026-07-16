import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { api, type ContextInfo } from "@/lib/api";
import { connectivity } from "@/lib/connectivity";
import { clusterScopedKeyPrefixes } from "@/lib/query-keys";

/** All kubeconfig contexts with their active flag. Cheap: no cluster calls. */
export function useContexts() {
  return useQuery({
    queryKey: ["contexts"],
    queryFn: api.contexts.list,
  });
}

/** Health poll cadence (FB-6 Story D): back off from 30s to 60s while the active
 *  context is unreachable so a down cluster is not probed at full cadence.
 *  Exported for unit tests. */
export function healthRefetchInterval(): number {
  return connectivity.isActiveUnreachable() ? 60_000 : 30_000;
}

/** Per-context connection health. Probes clusters, so it loads independently
 *  of the context list and refreshes periodically. Mirrors the active context's
 *  health into the connectivity store so the banner and poll backoff react even
 *  without an open watch stream. */
export function useContextsHealth() {
  const { data: contexts } = useContexts();
  const query = useQuery({
    queryKey: ["contexts", "health"],
    queryFn: api.contexts.health,
    refetchInterval: healthRefetchInterval,
  });

  const activeName = contexts?.find((c) => c.active)?.name;
  useEffect(() => {
    if (!query.data || !activeName) return;
    const active = query.data.find((h) => h.name === activeName);
    if (!active) return;
    if (active.reachable && active.authOK) {
      connectivity.setActiveUnreachable(false);
      connectivity.markEverConnected();
    } else {
      connectivity.setActiveUnreachable(true);
    }
  }, [query.data, activeName]);

  return query;
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
