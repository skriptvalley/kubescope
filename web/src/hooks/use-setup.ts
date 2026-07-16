import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";

import { api } from "@/lib/api";
import { connectivity } from "@/lib/connectivity";

/** The server's first-run / connectivity posture (FB-6). Polls every 10s while
 *  not ready so the starter page recovers on its own once the cluster comes up
 *  or a kubeconfig is mounted; stops polling once ready. Marks the connectivity
 *  store's ever-connected flag on the first ready state, which downgrades a later
 *  outage from the full-page starter to the in-app banner. */
export function useSetupState() {
  const query = useQuery({
    queryKey: ["setup"],
    queryFn: api.setup.state,
    refetchInterval: (q) => (q.state.data?.state !== "ready" ? 10_000 : false),
  });

  useEffect(() => {
    if (query.data?.state === "ready") connectivity.markEverConnected();
  }, [query.data?.state]);

  return query;
}
