import { Cable, X } from "lucide-react";

import { useStopPortForward } from "@/hooks/use-mutations";
import { usePortForwards } from "@/hooks/use-port-forwards";
import { formatAge } from "@/lib/age";
import type { PortForward } from "@/lib/api";

/** Where a forward points: a pod, or a Service whose ready endpoints it
 *  load-balances across (FB-13). */
function forwardTarget(f: PortForward): string {
  const name = f.targetKind === "service" ? f.service : f.pod;
  return `${f.namespace}/${name ?? "—"}:${f.remotePort}`;
}

/** Global active-forwards strip (Story 6.3): a persistent, unobtrusive bar that
 *  appears only when forwards exist, listing each forward's local→target
 *  mapping, context and uptime, with one-click stop. Service forwards also show
 *  their live backend count — the rotation shrinks as backing pods go away.
 *  Hosted in the layout so it is reachable from every page. */
export function ActiveForwardsPanel() {
  const { data } = usePortForwards();
  const stop = useStopPortForward();
  const forwards = data ?? [];
  if (forwards.length === 0) return null;

  return (
    <div className="shrink-0 border-t bg-muted/30 px-4 py-2" aria-label="Active port-forwards">
      <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <Cable className="h-3.5 w-3.5" />
        Active port-forwards ({forwards.length})
      </div>
      <ul className="mt-1.5 flex flex-wrap gap-2">
        {forwards.map((f) => (
          <li
            key={f.id}
            className="flex items-center gap-2 rounded-md border bg-background px-2 py-1 text-xs"
          >
            <span className="font-mono">
              127.0.0.1:{f.localPort} → {forwardTarget(f)}
            </span>
            {f.targetKind === "service" && (
              <span className="text-muted-foreground">
                {f.backends ?? 0} {(f.backends ?? 0) === 1 ? "endpoint" : "endpoints"}
              </span>
            )}
            <span className="text-muted-foreground">{f.context}</span>
            <span className="text-muted-foreground">up {formatAge(f.startedAt)}</span>
            <button
              type="button"
              onClick={() => stop.mutate(f.id)}
              aria-label={`Stop forward to ${forwardTarget(f)}`}
              className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
