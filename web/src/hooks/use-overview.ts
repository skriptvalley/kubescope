import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** Per-cluster overview for the active context. Refetches on context switch via
 *  the global invalidation in useSwitchContext. */
export function useOverview() {
  return useQuery({
    queryKey: ["overview"],
    queryFn: api.overview,
  });
}
