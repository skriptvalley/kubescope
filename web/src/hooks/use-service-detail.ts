import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { queryKeys } from "@/lib/query-keys";

/** A Service's typed detail (summary + resolved endpoints). Server-side
 *  aggregation the raw object can't carry — the backing pods split by readiness. */
export function useServiceDetail(namespace: string, name: string) {
  return useQuery({
    queryKey: queryKeys.serviceDetail(namespace, name),
    queryFn: () => api.services.detail(namespace, name),
  });
}
