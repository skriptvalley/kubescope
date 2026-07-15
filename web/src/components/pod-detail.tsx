import { Link } from "react-router-dom";

import { EventsPanel } from "@/components/events-panel";
import { StatusBadge } from "@/components/status-badge";
import { Badge } from "@/components/ui/badge";
import type { KubeObject } from "@/lib/api";
import { routeForKind } from "@/lib/workloads";
import { podStatusTone, type StatusTone } from "@/lib/workload-status";

// Narrow typed views of the parts of a Pod object this detail view renders. The
// generic get returns an untyped object (unknown-indexed); we cast to these
// shapes locally to render container/condition/placement detail.
interface ContainerStateDetail {
  reason?: string;
  message?: string;
  exitCode?: number;
  signal?: number;
  startedAt?: string;
}
interface ContainerState {
  running?: ContainerStateDetail;
  waiting?: ContainerStateDetail;
  terminated?: ContainerStateDetail;
}
interface ContainerStatus {
  name: string;
  image?: string;
  ready?: boolean;
  restartCount?: number;
  state?: ContainerState;
}
interface PodCondition {
  type?: string;
  status?: string;
  reason?: string;
  lastTransitionTime?: string;
}
interface SpecContainer {
  name: string;
  image?: string;
}
interface PodObject {
  metadata?: {
    ownerReferences?: { kind?: string; name?: string }[];
  };
  spec?: {
    nodeName?: string;
    containers?: SpecContainer[];
    initContainers?: SpecContainer[];
  };
  status?: {
    phase?: string;
    podIP?: string;
    qosClass?: string;
    conditions?: PodCondition[];
    containerStatuses?: ContainerStatus[];
    initContainerStatuses?: ContainerStatus[];
    ephemeralContainerStatuses?: ContainerStatus[];
  };
}

/** Dedicated Pod detail: placement, conditions and a per-container breakdown,
 *  layered on the generic detail page. The raw YAML tab lives on the parent. */
export function PodDetail({
  object,
  namespace,
  name,
}: {
  object: KubeObject;
  namespace?: string;
  name: string;
}) {
  const pod = object as unknown as PodObject;
  const spec = pod.spec ?? {};
  const status = pod.status ?? {};
  const owner = pod.metadata?.ownerReferences?.[0];
  const ownerRoute =
    owner?.kind && owner.name && namespace
      ? routeForKind(owner.kind, namespace, owner.name)
      : undefined;

  return (
    <div className="space-y-6">
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Field label="Phase" value={status.phase ?? "—"} />
        <Field label="Node" value={spec.nodeName ?? "—"} />
        <Field label="Pod IP" value={status.podIP ?? "—"} />
        <Field label="QoS class" value={status.qosClass ?? "—"} />
        <div>
          <dt className="text-xs text-muted-foreground">Controlled by</dt>
          <dd className="mt-0.5 break-all text-sm font-medium">
            {owner ? (
              ownerRoute ? (
                <Link to={ownerRoute} className="underline-offset-4 hover:underline">
                  {owner.kind}/{owner.name}
                </Link>
              ) : (
                `${owner.kind}/${owner.name}`
              )
            ) : (
              "—"
            )}
          </dd>
        </div>
      </dl>

      <ContainerGroup title="Init containers" statuses={status.initContainerStatuses} specs={spec.initContainers} />
      <ContainerGroup title="Containers" statuses={status.containerStatuses} specs={spec.containers} />
      <ContainerGroup title="Ephemeral containers" statuses={status.ephemeralContainerStatuses} specs={[]} hideWhenEmpty />

      <section className="space-y-2">
        <h3 className="text-sm font-semibold">Conditions</h3>
        {(status.conditions ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">None</p>
        ) : (
          <ul className="space-y-1 text-sm" data-testid="pod-conditions">
            {status.conditions!.map((c) => (
              <li key={c.type} className="flex items-center gap-2">
                <Badge variant={c.status === "True" ? "secondary" : "outline"} className="font-normal">
                  {c.type}={c.status}
                </Badge>
                {c.reason && <span className="text-muted-foreground">{c.reason}</span>}
              </li>
            ))}
          </ul>
        )}
      </section>

      <EventsPanel kind="Pod" namespace={namespace} name={name} />
    </div>
  );
}

function ContainerGroup({
  title,
  statuses,
  specs,
  hideWhenEmpty,
}: {
  title: string;
  statuses?: ContainerStatus[];
  specs?: SpecContainer[];
  hideWhenEmpty?: boolean;
}) {
  const list = statuses ?? [];
  if (list.length === 0 && hideWhenEmpty) return null;
  const imageForName = new Map((specs ?? []).map((c) => [c.name, c.image]));

  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold">{title}</h3>
      {list.length === 0 ? (
        <p className="text-sm text-muted-foreground">None</p>
      ) : (
        <ul className="space-y-2" data-testid={`containers-${slug(title)}`}>
          {list.map((cs) => {
            const state = containerStateLabel(cs.state);
            return (
              <li key={cs.name} className="rounded-md border p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{cs.name}</span>
                  <StatusBadge tone={state.tone}>{state.label}</StatusBadge>
                  {cs.ready ? (
                    <Badge variant="outline" className="font-normal">
                      ready
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="font-normal text-muted-foreground">
                      not ready
                    </Badge>
                  )}
                  <span className="text-xs text-muted-foreground">
                    restarts: {cs.restartCount ?? 0}
                  </span>
                </div>
                <p className="mt-1 break-all font-mono text-xs text-muted-foreground">
                  {cs.image ?? imageForName.get(cs.name) ?? "—"}
                </p>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/** Renders a container's current state as a toned label (waiting/running/
 *  terminated with reason), so a crash reason is legible at a glance. */
function containerStateLabel(state?: ContainerState): { tone: StatusTone; label: string } {
  if (state?.running) return { tone: "ok", label: "Running" };
  if (state?.waiting) {
    const reason = state.waiting.reason ?? "Waiting";
    return { tone: podStatusTone(reason), label: reason };
  }
  if (state?.terminated) {
    const t = state.terminated;
    const reason = t.reason ?? (t.signal ? `Signal:${t.signal}` : `ExitCode:${t.exitCode ?? 0}`);
    return { tone: reason === "Completed" ? "ok" : "warn", label: reason };
  }
  return { tone: "progress", label: "Unknown" };
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-all text-sm font-medium">{value}</dd>
    </div>
  );
}

function slug(s: string): string {
  return s.toLowerCase().replace(/\s+/g, "-");
}
