import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** Per-resource-type counts for the sidebar (ADR-0009). Best-effort: a slow or
 *  partial response degrades to "no count" rather than an error, so this never
 *  blocks the nav. Refetches on context switch via the global invalidation. */
export function useResourceCounts() {
  return useQuery({
    queryKey: ["counts"],
    queryFn: api.counts,
    // Counts fan out over every type; keep them fresh-ish without hammering.
    staleTime: 30_000,
    retry: false,
  });
}
