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
  const start = useStartPortForward();

  return (
    <ForwardForm
      portLabel="Pod port"
      portListId="pod-declared-ports"
      ports={ports}
      initialPort={ports[0]}
      note={
        ports.length > 0 ? (
          <>
            Declared ports: <span className="font-mono">{ports.join(", ")}</span>
          </>
        ) : null
      }
      pending={start.isPending}
      error={start.isError ? start.error : null}
      onStart={(remotePort, localPort) => start.mutate({ namespace, pod, remotePort, localPort })}
    />
  );
}

/** Start-a-forward control on service detail (FB-13). One loopback listener
 *  fronts one forward per ready endpoint pod and hands each new TCP connection
 *  to the next one — the same per-connection granularity ClusterIP gives, not an
 *  L7 proxy. Hidden in read-only mode by the caller; the server blocks the start
 *  too, exactly as for a pod forward. */
export function ServicePortForwardControls({
  namespace,
  service,
  ports,
  readyEndpoints,
}: {
  namespace: string;
  service: string;
  ports: number[];
  readyEndpoints: number;
}) {
  const start = useStartPortForward();
  const noBackends = readyEndpoints === 0;

  return (
    <ForwardForm
      portLabel="Service port"
      portListId="service-declared-ports"
      ports={ports}
      initialPort={ports[0]}
      disabled={noBackends}
      description={
        noBackends
          ? "No ready endpoints to forward to."
          : `Port-forward (load-balanced across ${readyEndpoints} ${
              readyEndpoints === 1 ? "endpoint" : "endpoints"
            }) — each new connection goes to the next ready pod.`
      }
      pending={start.isPending}
      error={start.isError ? start.error : null}
      onStart={(servicePort, localPort) =>
        start.mutate({ namespace, service, servicePort, localPort })
      }
    />
  );
}

/** The shared start-a-forward form. Both targets ask the same two questions —
 *  which remote port, and which local port (blank = auto) — and differ only in
 *  what the remote port means. */
function ForwardForm({
  portLabel,
  portListId,
  ports,
  initialPort,
  description,
  note,
  disabled = false,
  pending,
  error,
  onStart,
}: {
  portLabel: string;
  portListId: string;
  ports: number[];
  initialPort?: number;
  description?: string;
  note?: React.ReactNode;
  disabled?: boolean;
  pending: boolean;
  error: unknown;
  onStart: (remotePort: number, localPort: number) => void;
}) {
  const [remotePort, setRemotePort] = useState(initialPort ? String(initialPort) : "");
  const [localPort, setLocalPort] = useState("");

  const remote = Number(remotePort);
  const validRemote = Number.isInteger(remote) && remote >= 1 && remote <= 65535;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!validRemote || disabled) return;
    const local = localPort === "" ? 0 : Number(localPort);
    onStart(remote, Number.isInteger(local) && local >= 0 && local <= 65535 ? local : 0);
  };

  return (
    <section className="space-y-2" aria-label="Port forwarding">
      <h3 className="font-display text-sm font-medium">Port forwarding</h3>
      {description && <p className="text-[11.5px] text-muted-foreground">{description}</p>}
      <form className="flex flex-wrap items-end gap-2.5" onSubmit={submit}>
        <label className="flex flex-col gap-1">
          <span className="text-[11.5px] font-medium text-muted-foreground">{portLabel}</span>
          <input
            type="number"
            min={1}
            max={65535}
            list={portListId}
            className="h-8 w-[104px] rounded-md border border-input bg-transparent px-2.5 font-mono text-[12.5px] outline-none transition-[box-shadow,border-color] focus:border-ring focus:ring-[3px] focus:ring-ring/40"
            value={remotePort}
            onChange={(e) => setRemotePort(e.target.value)}
            aria-label={portLabel}
            placeholder="e.g. 8080"
          />
          {ports.length > 0 && (
            <datalist id={portListId}>
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

        <Button type="submit" size="sm" disabled={!validRemote || disabled || pending}>
          {pending ? "Starting…" : "Forward"}
        </Button>

        {note && <span className="pb-1.5 text-[11.5px] text-muted-foreground">{note}</span>}
      </form>

      {error != null && (
        <p className="text-sm text-destructive" role="alert">
          {forwardErrorText(error)}
        </p>
      )}
    </section>
  );
}

function forwardErrorText(error: unknown): string {
  if (error instanceof ApiError) return `${error.message} (${error.code})`;
  return error instanceof Error ? error.message : "Failed to start forward";
}
