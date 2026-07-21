import { Link } from "react-router-dom";

import { DetailField, DetailGrid, DetailSection } from "@/components/detail-ui";
import { EventsPanel } from "@/components/events-panel";
import { PortForwardControls } from "@/components/port-forward-controls";
import { StatusBadge } from "@/components/status-badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useReadOnly } from "@/hooks/use-config";
import { formatAge } from "@/lib/age";
import type { KubeObject } from "@/lib/api";
import { restartClass, toneStyles } from "@/lib/tone-style";
import { cn } from "@/lib/utils";
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
    creationTimestamp?: string;
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

interface ContainerRow {
  status: ContainerStatus;
  kind: "" | "init" | "ephemeral";
  imageFallback?: string;
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
  const readOnly = useReadOnly();
  const pod = object as unknown as PodObject;
  const spec = pod.spec ?? {};
  const status = pod.status ?? {};
  const owner = pod.metadata?.ownerReferences?.[0];
  const ownerRoute =
    owner?.kind && owner.name && namespace
      ? routeForKind(owner.kind, namespace, owner.name)
      : undefined;

  const specImages = new Map(
    [...(spec.initContainers ?? []), ...(spec.containers ?? [])].map((c) => [c.name, c.image]),
  );
  const rows: ContainerRow[] = [
    ...(status.initContainerStatuses ?? []).map((s) => ({ status: s, kind: "init" as const, imageFallback: specImages.get(s.name) })),
    ...(status.containerStatuses ?? []).map((s) => ({ status: s, kind: "" as const, imageFallback: specImages.get(s.name) })),
    ...(status.ephemeralContainerStatuses ?? []).map((s) => ({ status: s, kind: "ephemeral" as const, imageFallback: specImages.get(s.name) })),
  ];

  return (
    <div className="flex flex-col gap-[18px]">
      <DetailGrid>
        <DetailField label="Phase" value={status.phase ?? "—"} />
        <DetailField label="Node">
          <span className="font-mono">{spec.nodeName ?? "—"}</span>
        </DetailField>
        <DetailField label="Pod IP">
          <span className="font-mono">{status.podIP ?? "—"}</span>
        </DetailField>
        <DetailField label="QoS class" value={status.qosClass ?? "—"} />
        <DetailField label="Controlled by">
          {owner ? (
            ownerRoute ? (
              <Link to={ownerRoute} className="font-mono text-primary hover:underline">
                {owner.kind}/{owner.name}
              </Link>
            ) : (
              <span className="font-mono">
                {owner.kind}/{owner.name}
              </span>
            )
          ) : (
            "—"
          )}
        </DetailField>
        <DetailField label="Age">
          <span className="font-mono">{formatAge(pod.metadata?.creationTimestamp)}</span>
        </DetailField>
      </DetailGrid>

      <DetailSection title="Containers">
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">None</p>
        ) : (
          <div className="overflow-hidden rounded-lg bg-card shadow-ring">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Container</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Ready</TableHead>
                  <TableHead className="text-right">Restarts</TableHead>
                  <TableHead>Image</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody data-testid="containers">
                {rows.map((row) => {
                  const cs = row.status;
                  const state = containerStateLabel(cs.state);
                  const restarts = cs.restartCount ?? 0;
                  return (
                    <TableRow key={`${row.kind}-${cs.name}`} className="hover:bg-transparent">
                      <TableCell>
                        <span className="font-mono text-xs font-medium">{cs.name}</span>
                        {row.kind && (
                          <span className="ml-1.5 text-[11px] text-muted-foreground">{row.kind}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <StatusBadge tone={state.tone} dot>
                          {state.label}
                        </StatusBadge>
                      </TableCell>
                      <TableCell>
                        <span
                          className={cn(
                            "inline-flex rounded-sm border px-1.5 py-px text-[11.5px]",
                            cs.ready ? "text-foreground" : "text-muted-foreground",
                          )}
                        >
                          {cs.ready ? "ready" : "not ready"}
                        </span>
                      </TableCell>
                      <TableCell className="text-right">
                        <span className={cn("font-mono text-xs", restartClass(restarts))}>{restarts}</span>
                      </TableCell>
                      <TableCell>
                        <span className="break-all font-mono text-[11.5px] text-muted-foreground">
                          {cs.image ?? row.imageFallback ?? "—"}
                        </span>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </DetailSection>

      <DetailSection title="Conditions">
        {(status.conditions ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">None</p>
        ) : (
          <div className="flex flex-wrap gap-1.5" data-testid="pod-conditions">
            {status.conditions!.map((c) => (
              <ConditionChip key={c.type} type={c.type} status={c.status} reason={c.reason} />
            ))}
          </div>
        )}
      </DetailSection>

      {!readOnly && <PortForwardControls namespace={namespace ?? ""} pod={name} object={object} />}

      <EventsPanel kind="Pod" namespace={namespace} name={name} />
    </div>
  );
}

/** A condition chip (reason in the title). True → secondary; PodScheduled=False
 *  is a transitional/pending state → highlight (pumpkin); any other False/Unknown
 *  → destructive. Routes non-True through the centralized tone map so it matches
 *  the Dusk conditions row without duplicating the tint classes. */
function ConditionChip({
  type,
  status,
  reason,
}: {
  type?: string;
  status?: string;
  reason?: string;
}) {
  return (
    <span
      title={reason || undefined}
      className={cn(
        "inline-flex items-center rounded-sm px-2 py-0.5 text-xs font-medium",
        conditionPill(type, status),
      )}
    >
      {type}={status}
    </span>
  );
}

/** Tint classes for a pod condition chip (see ConditionChip). */
function conditionPill(type?: string, status?: string): string {
  if (status === "True") return "bg-secondary text-secondary-foreground";
  if (type === "PodScheduled") return toneStyles.progress.pill; // Unschedulable = pending
  return toneStyles.warn.pill;
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
    return { tone: reason === "Completed" ? "neutral" : "warn", label: reason };
  }
  return { tone: "progress", label: "Unknown" };
}
