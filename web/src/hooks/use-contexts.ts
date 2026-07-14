import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type ContextInfo } from "@/lib/api";

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

/** Switch the active context, then invalidate every query so all views refetch
 *  against the new cluster. */
export function useSwitchContext() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.contexts.switch,
    onSuccess: (items: ContextInfo[]) => {
      queryClient.setQueryData(["contexts"], items);
      void queryClient.invalidateQueries();
    },
  });
}
