import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** A typed workload summary list for one kind + namespace scope. T is the
 *  matching *Summary type supplied by the caller. */
export function useWorkloadSummary<T>(resource: string, namespace?: string) {
  return useQuery({
    queryKey: ["workload-summary", resource, namespace ?? ""],
    queryFn: () => api.workloads.list<T>(resource, namespace),
  });
}

/** The pods a controller owns, resolved server-side (selector + ownerRef). */
export function useOwnedPods(resource: string, namespace: string, name: string, enabled = true) {
  return useQuery({
    queryKey: ["owned-pods", resource, namespace, name],
    queryFn: () => api.workloads.ownedPods(resource, namespace, name),
    enabled,
  });
}

/** The Jobs a CronJob owns (its active + recent runs). */
export function useOwnedJobs(namespace: string, name: string, enabled = true) {
  return useQuery({
    queryKey: ["owned-jobs", namespace, name],
    queryFn: () => api.workloads.ownedJobs(namespace, name),
    enabled,
  });
}
