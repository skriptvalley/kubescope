import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { clusterScopedKeyPrefixes, queryKeys } from "@/lib/query-keys";

/** A source mutation (or a rescan picking up a changed dir) can repoint the
 *  ACTIVE context at a different cluster server-side, so it needs the same
 *  treatment as a context switch (FB-2): refetch every mounted view in place
 *  first — a removed query has no observer to refetch — then drop the unmounted
 *  cluster-scoped caches so later navigation refetches instead of flashing the
 *  previous cluster's data. Anything narrower leaves e.g. the Overview showing
 *  the old cluster indefinitely (no refetchInterval on those views). */
function invalidateKubeconfigScope(queryClient: QueryClient): void {
  void queryClient.invalidateQueries();
  for (const key of clusterScopedKeyPrefixes) {
    queryClient.removeQueries({ queryKey: key, type: "inactive" });
  }
}

/** The kubeconfig source registry (FB-8). Registry-scoped, so it is not dropped
 *  on a context switch; a per-request server scan makes a refetch a fresh rescan. */
export function useKubeconfigSources() {
  return useQuery({
    queryKey: queryKeys.kubeconfigs(),
    queryFn: api.kubeconfigs.list,
  });
}

/** Register a new kubeconfig source by absolute path. On success, invalidates the
 *  full kubeconfig scope so the listing, setup posture, contexts and nav refresh. */
export function useAddKubeconfigSource() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (path: string) => api.kubeconfigs.add(path),
    onSuccess: () => invalidateKubeconfigScope(queryClient),
  });
}

/** Remove a registered kubeconfig source by id. Invalidates the full scope. */
export function useRemoveKubeconfigSource() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.kubeconfigs.remove(id),
    onSuccess: () => invalidateKubeconfigScope(queryClient),
  });
}

/** A rescan is a pure refetch (the server scans per request), so it just
 *  invalidates the same scope the mutations do. */
export function useRescanKubeconfigSources() {
  const queryClient = useQueryClient();
  return () => invalidateKubeconfigScope(queryClient);
}
