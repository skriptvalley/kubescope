import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ExternalLink, RefreshCw } from "lucide-react";
import { useSyncExternalStore } from "react";

import { Button } from "@/components/ui/button";
import { useContexts, useContextsHealth } from "@/hooks/use-contexts";
import { connectivity } from "@/lib/connectivity";

// A persistent notice shown when the active context has gone unreachable while
// the app is already running (FB-6 Story D). It is the in-app counterpart to the
// full-page starter: the starter covers "never connected", this covers a live
// session whose cluster degraded. Visible when the active context's health probe
// is unhealthy or a watch stream reported the cluster unreachable; auto-hides on
// recovery. First-run states are handled by the starter gate, not here.
export function ClusterBanner() {
  const queryClient = useQueryClient();
  const storeUnreachable = useSyncExternalStore(
    connectivity.subscribe,
    connectivity.isActiveUnreachable,
  );
  const everConnected = useSyncExternalStore(
    connectivity.subscribe,
    connectivity.hasEverConnected,
  );
  const { data: contexts } = useContexts();
  const { data: health } = useContextsHealth();

  const activeName = contexts?.find((c) => c.active)?.name;
  const activeHealth = activeName ? health?.find((h) => h.name === activeName) : undefined;
  const healthUnreachable = activeHealth
    ? !(activeHealth.reachable && activeHealth.authOK)
    : false;

  // Before the first successful connection the full-page starter owns the
  // unreachable story; stacking the banner on top of it would be noise.
  if (!everConnected) return null;
  if (!healthUnreachable && !storeUnreachable) return null;

  const reason = activeHealth?.reason;
  const guidance = activeHealth?.guidance ?? activeHealth?.error;
  const docURL = activeHealth?.docURL;

  const retry = () => {
    void queryClient.invalidateQueries({ queryKey: ["contexts", "health"] });
    void queryClient.invalidateQueries({ queryKey: ["setup"] });
  };

  return (
    <div
      role="status"
      data-testid="cluster-banner"
      className="flex shrink-0 items-start gap-2 border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-sm text-destructive"
    >
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span>
          Cluster unreachable{activeName ? ` — context ${activeName}` : ""}
          {reason ? ` (${reason})` : ""}. Live updates are paused; the app will
          reconnect automatically.
        </span>
        {guidance && <span className="text-xs opacity-90">{guidance}</span>}
        <div className="flex items-center gap-3">
          <Button variant="outline" size="sm" onClick={retry}>
            <RefreshCw className="h-3.5 w-3.5" />
            Retry
          </Button>
          {docURL && (
            <a
              href={docURL}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-xs font-medium underline underline-offset-2"
            >
              Learn more
              <ExternalLink className="h-3 w-3" />
            </a>
          )}
        </div>
      </div>
    </div>
  );
}
