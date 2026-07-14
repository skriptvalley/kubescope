import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

export function useNodes() {
  return useQuery({
    queryKey: ["nodes"],
    queryFn: api.nodes.list,
  });
}
