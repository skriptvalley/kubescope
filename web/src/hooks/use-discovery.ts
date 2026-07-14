import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** The active cluster's API groups/resources, driving the sidebar nav. Cached
 *  per cluster (dropped on context switch); refreshed explicitly to pick up a
 *  newly-installed CRD. */
export function useDiscovery() {
  return useQuery({
    queryKey: ["discovery"],
    queryFn: () => api.resources.discovery(),
  });
}
