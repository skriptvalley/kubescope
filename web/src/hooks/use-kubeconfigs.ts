import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { queryKeys } from "@/lib/query-keys";

/** Invalidate everything a kubeconfig-source change (or a rescan) can affect: the
 *  source listing itself, the server's setup posture, the context list + health,
 *  and discovery. Discovery is included so the sidebar nav re-derives when a new
 *  source repoints the active cluster (the stale-nav gap the old control had). */
function invalidateKubeconfigScope(queryClient: QueryClient): void {
  void queryClient.invalidateQueries({ queryKey: queryKeys.kubeconfigs() });
  void queryClient.invalidateQueries({ queryKey: ["setup"] });
  void queryClient.invalidateQueries({ queryKey: ["contexts"] });
  void queryClient.invalidateQueries({ queryKey: ["contexts", "health"] });
  void queryClient.invalidateQueries({ queryKey: ["discovery"] });
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
