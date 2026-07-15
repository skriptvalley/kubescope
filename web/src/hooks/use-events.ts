import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

/** Events for a single object, filtered server-side by involvedObject. */
export function useEvents(ref: { namespace?: string; kind: string; name: string }, enabled = true) {
  return useQuery({
    queryKey: ["events", ref.kind, ref.namespace ?? "", ref.name],
    queryFn: () => api.events(ref),
    enabled: enabled && ref.name !== "",
  });
}
