import { useState } from "react";

import { Button } from "@/components/ui/button";
import { useStartPortForward } from "@/hooks/use-mutations";
import { ApiError, type KubeObject } from "@/lib/api";

interface SpecPort {
  containerPort?: number;
}

/** Distinct containerPorts declared on the pod, offered as quick-fill options. */
function declaredPorts(object?: KubeObject): number[] {
  const spec = (object?.spec ?? {}) as { containers?: { ports?: SpecPort[] }[] };
  const seen = new Set<number>();
  for (const c of spec.containers ?? []) {
    for (const p of c.ports ?? []) {
      if (typeof p.containerPort === "number") seen.add(p.containerPort);
    }
  }
  return [...seen];
}

/** Start-a-forward control on pod detail (Story 6.3). Hidden in read-only mode
 *  by the caller; the server also blocks the start (ADR-0005). The running
 *  forwards themselves live in the global active-forwards panel. */
export function PortForwardControls({
  namespace,
  pod,
  object,
}: {
  namespace: string;
  pod: string;
  object?: KubeObject;
}) {
  const ports = declaredPorts(object);
  const [remotePort, setRemotePort] = useState(ports[0] ? String(ports[0]) : "");
  const [localPort, setLocalPort] = useState("");
  const start = useStartPortForward();

  const remote = Number(remotePort);
  const validRemote = Number.isInteger(remote) && remote >= 1 && remote <= 65535;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!validRemote) return;
    const local = localPort === "" ? 0 : Number(localPort);
    start.mutate({
      namespace,
      pod,
      remotePort: remote,
      localPort: Number.isInteger(local) && local >= 0 && local <= 65535 ? local : 0,
    });
  };

  return (
    <section className="space-y-2" aria-label="Port forwarding">
      <h3 className="font-display text-sm font-medium">Port forwarding</h3>
      <form className="flex flex-wrap items-end gap-2.5" onSubmit={submit}>
        <label className="flex flex-col gap-1">
          <span className="text-[11.5px] font-medium text-muted-foreground">Pod port</span>
          <input
            type="number"
            min={1}
            max={65535}
            list="pod-declared-ports"
            className="h-8 w-[104px] rounded-md border border-input bg-transparent px-2.5 font-mono text-[12.5px] outline-none transition-[box-shadow,border-color] focus:border-ring focus:ring-[3px] focus:ring-ring/40"
            value={remotePort}
            onChange={(e) => setRemotePort(e.target.value)}
            aria-label="Pod port"
            placeholder="e.g. 8080"
          />
          {ports.length > 0 && (
            <datalist id="pod-declared-ports">
              {ports.map((p) => (
                <option key={p} value={p} />
              ))}
            </datalist>
          )}
        </label>

        <label className="flex flex-col gap-1">
          <span className="text-[11.5px] font-medium text-muted-foreground">Local port</span>
          <input
            type="number"
            min={0}
            max={65535}
            className="h-8 w-[104px] rounded-md border border-input bg-transparent px-2.5 font-mono text-[12.5px] outline-none transition-[box-shadow,border-color] focus:border-ring focus:ring-[3px] focus:ring-ring/40"
            value={localPort}
            onChange={(e) => setLocalPort(e.target.value)}
            aria-label="Local port"
            placeholder="auto"
          />
        </label>

        <Button type="submit" size="sm" disabled={!validRemote || start.isPending}>
          {start.isPending ? "Starting…" : "Forward"}
        </Button>

        {ports.length > 0 && (
          <span className="pb-1.5 text-[11.5px] text-muted-foreground">
            Declared ports: <span className="font-mono">{ports.join(", ")}</span>
          </span>
        )}
      </form>

      {start.isError && (
        <p className="text-sm text-destructive" role="alert">
          {forwardErrorText(start.error)}
        </p>
      )}
    </section>
  );
}

function forwardErrorText(error: unknown): string {
  if (error instanceof ApiError) return `${error.message} (${error.code})`;
  return error instanceof Error ? error.message : "Failed to start forward";
}
