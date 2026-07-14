import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** Namespace names backing the list-page selector. */
export function useNamespaces() {
  return useQuery({
    queryKey: ["namespaces"],
    queryFn: api.namespaces.list,
  });
}
