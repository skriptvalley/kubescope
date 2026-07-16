import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { queryKeys } from "@/lib/query-keys";

/** Minimum query length before a search fires — keeps a single keystroke from
 *  sweeping every type in the cluster. */
export const MIN_SEARCH_LENGTH = 2;

/** Cross-resource name search in the active context. Fires only once the query
 *  is long enough and the caller enables it (the dropdown is open). */
export function useSearch(query: string, enabled: boolean) {
  const trimmed = query.trim();
  return useQuery({
    queryKey: queryKeys.search(trimmed),
    queryFn: () => api.search(trimmed, 20),
    enabled: enabled && trimmed.length >= MIN_SEARCH_LENGTH,
    staleTime: 10_000,
  });
}
