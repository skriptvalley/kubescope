import { useQuery } from "@tanstack/react-query";

import { api, type PodMetrics } from "@/lib/api";

/** Pod CPU/Memory from metrics-server (ADR-0009). Returns a map of
 *  `namespace/name` → usage plus `available` (false when metrics-server is
 *  absent, so callers render "—"). Polls slowly; metrics change gradually and
 *  this is advisory data layered onto the live pod list. */
export function usePodMetrics(namespace?: string) {
  const query = useQuery({
    queryKey: ["metrics", "pods", namespace ?? "all"],
    queryFn: () => api.metrics.pods(namespace),
    refetchInterval: 15_000,
    // Metrics are best-effort; a slow/absent metrics API must not spam retries.
    retry: false,
    staleTime: 10_000,
  });

  const available = query.data?.available ?? false;
  const byPod = new Map<string, PodMetrics>();
  for (const m of query.data?.items ?? []) {
    byPod.set(`${m.namespace}/${m.name}`, m);
  }
  return { available, byPod, query };
}
