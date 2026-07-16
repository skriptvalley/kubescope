import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { connectivity } from "@/lib/connectivity";
import { queryKeys } from "@/lib/query-keys";

/** Port-forward poll cadence (FB-6 Story D): back off from 5s to 30s while the
 *  active context is unreachable. Exported for unit tests. */
export function portForwardsRefetchInterval(): number {
  return connectivity.isActiveUnreachable() ? 30_000 : 5_000;
}

/** Active port-forwards for the current context (Story 6.3). Refetched on an
 *  interval so the uptime column ticks and a forward that died server-side
 *  (pod deleted) drops out of the list without a manual refresh. Keyed under a
 *  cluster-scoped prefix, so a context switch refetches it (the panel is always
 *  mounted) — forwards are torn down server-side on switch. */
export function usePortForwards() {
  return useQuery({
    queryKey: queryKeys.portForwards(),
    queryFn: api.portForwards.list,
    refetchInterval: portForwardsRefetchInterval,
  });
}
