import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** ResourceQuota bars for a namespace (ADR-0009). An empty list is normal (the
 *  section hides); only enabled once a namespace is known. */
export function useNamespaceQuotas(namespace: string | undefined) {
  return useQuery({
    queryKey: ["quotas", namespace],
    queryFn: () => api.namespaces.quotas(namespace!),
    enabled: !!namespace,
  });
}
