import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api, type ResourceRef } from "@/lib/api";
import { queryKeys } from "@/lib/query-keys";

// Mutation hooks (Sprint 5). Each wraps a single write call and invalidates the
// caches its result affects, so the view reflects the change even before (or
// without) an SSE event. Server-side read-only enforcement means a mutation
// attempted while read-only will reject with a 403 ApiError regardless of these
// hooks — the UI disables the controls, but the guard is the server's.

/** Apply an edited manifest. On success the object and its YAML are refetched so
 *  the view (including the new resourceVersion) is current for the next edit. */
export function useApplyResource(ref: ResourceRef) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (yaml: string) => api.resources.apply(ref, yaml),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.resourceObject(ref) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.resourceYaml(ref) });
    },
  });
}

/** Delete an object. Callers supply navigation on success (there is no object to
 *  return to); the list cache is invalidated so the row disappears. */
export function useDeleteResource(ref: ResourceRef, onSuccess?: () => void) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.resources.delete(ref),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.resourceList(ref) });
      onSuccess?.();
    },
  });
}

/** Scale a controller. The summary refetches so the replica count updates. */
export function useScaleWorkload(resource: string, namespace: string, name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (replicas: number) => api.workloads.scale(resource, namespace, name, replicas),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.workloadSummary(resource, namespace) });
    },
  });
}

/** Rollout-restart a controller. The summary refetches so rollout status moves. */
export function useRestartWorkload(resource: string, namespace: string, name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => api.workloads.restart(resource, namespace, name),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.workloadSummary(resource, namespace) });
    },
  });
}

/** Cordon/uncordon a node; the node list refetches so schedulability updates. */
export function useSetNodeSchedulable() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ name, cordon }: { name: string; cordon: boolean }) =>
      api.nodes.setSchedulable(name, cordon),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["nodes"] }),
  });
}

/** Drain a node; returns the per-pod result and refetches the node list. */
export function useDrainNode() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.nodes.drain(name),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["nodes"] }),
  });
}
