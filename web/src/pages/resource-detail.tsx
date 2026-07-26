import { ChevronRight, Trash2 } from "lucide-react";
import { lazy, Suspense, useState } from "react";
import { Link, useParams } from "react-router-dom";

import { ConfigMapDetail } from "@/components/configmap-detail";
import { ControllerDetail } from "@/components/controller-detail";
import { DeleteResourceButton } from "@/components/resource-actions";
import { ErrorState } from "@/components/error-state";
import { ExecTerminal } from "@/components/exec-terminal";
import { IngressDetail } from "@/components/ingress-detail";
import { LiveBadge } from "@/components/live-badge";
import { LogViewer } from "@/components/log-viewer";
import { NamespaceDetail } from "@/components/namespace-detail";
import { PodDetail } from "@/components/pod-detail";
import { BindingDetail, RoleDetail, ServiceAccountDetail } from "@/components/rbac-detail";
import { SecretDetail } from "@/components/secret-detail";
import { ServiceDetail } from "@/components/service-detail";
import { StatusBadge } from "@/components/status-badge";
import {
  PersistentVolumeClaimDetail,
  PersistentVolumeDetail,
  StorageClassDetail,
} from "@/components/storage-detail";
import { YamlTab } from "@/components/yaml-tab";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useReadOnly } from "@/hooks/use-config";
import { useLiveResourceObject } from "@/hooks/use-stream";
import { formatAge } from "@/lib/age";
import { type KubeObject } from "@/lib/api";
import { podDisplayStatus, type PodStatusObject } from "@/lib/pod-status";
import { detailKind, isSecret as isSecretRef } from "@/lib/resource-views";
import { cn } from "@/lib/utils";
import { workloadKind } from "@/lib/workloads";
import { podStatusTone, type StatusTone } from "@/lib/workload-status";

// The graph pulls in Cytoscape + fcose, so it is code-split: the tab is free
// until someone opens it, and first paint never pays for the renderer
// (FB-14/ADR-0011). Still bundled, never fetched from a CDN (ADR-0002).
const ResourceGraph = lazy(() => import("@/components/resource-graph"));

type Tab = "summary" | "logs" | "terminal" | "graph" | "yaml";

export function ResourceDetailPage() {
  const params = useParams();
  const group = params.group ?? "";
  const version = params.version ?? "";
  const resource = params.resource ?? "";
  const namespace = params.namespace; // undefined for cluster-scoped objects
  const name = params.name ?? "";

  const ref = { group, version, resource, namespace, name };
  const [tab, setTab] = useState<Tab>("summary");
  const readOnly = useReadOnly();
  const workload = workloadKind({ group, version, resource });
  const isPod = workload?.kind === "Pod";
  const isSecret = isSecretRef(ref);
  const detail = detailKind(ref);
  // Controller detail resolves its own data and never renders the raw object, so
  // skip the object fetch (and its stream) for those kinds — the YAML tab uses a
  // separate query.
  const object = useLiveResourceObject(ref, !workload?.controller);

  const kind = workload?.kind ?? object.data?.kind ?? resource;
  const isNamespace = detail === "namespace";
  const listRoute = `/resources/${group}/${version}/${resource}`;
  const kindPlural = resource.charAt(0).toUpperCase() + resource.slice(1);
  const headerBadge = deriveHeaderBadge(isPod, isNamespace, object.data);

  return (
    <div className="flex flex-col gap-3.5">
      <nav aria-label="Breadcrumb" className="flex items-center gap-1.5 text-[12.5px] text-muted-foreground">
        <Link to={listRoute} className="text-muted-foreground no-underline hover:text-primary hover:underline">
          {kindPlural}
        </Link>
        {namespace && (
          <>
            <ChevronRight className="h-3 w-3 opacity-50" />
            <Link
              to={`/resources/core/v1/namespaces/${encodeURIComponent(namespace)}`}
              className="text-muted-foreground no-underline hover:text-primary hover:underline"
            >
              {namespace}
            </Link>
          </>
        )}
        <ChevronRight className="h-3 w-3 opacity-50" />
        <span className="font-mono text-xs text-foreground">{name}</span>
      </nav>

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="break-all font-display text-[19px] font-semibold tracking-[-0.02em]">{name}</h1>
            {headerBadge && (
              <StatusBadge tone={headerBadge.tone} dot>
                {headerBadge.label}
              </StatusBadge>
            )}
          </div>
          <p className="mt-1 text-[13px] text-muted-foreground">
            {kind} · {group === "core" ? "core" : group}/{version}
            {namespace && (
              <>
                {" · namespace "}
                <Link
                  to={`/resources/core/v1/namespaces/${encodeURIComponent(namespace)}`}
                  className="text-primary hover:underline"
                >
                  {namespace}
                </Link>
              </>
            )}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {!workload?.controller && <LiveBadge status={object.streamStatus} />}
          {!readOnly && (
            <DeleteResourceButton
              refx={ref}
              kind={kind}
              label={isNamespace ? "Delete namespace" : "Delete"}
              cascade={
                isNamespace ? (
                  <>
                    Everything in <span className="font-mono text-xs">{name}</span> is deleted with it —
                    all pods, services, config maps and secrets — and finalizers may hold the namespace in
                    Terminating.
                  </>
                ) : undefined
              }
            />
          )}
        </div>
      </div>

      {object.deleted && (
        <Alert variant="destructive">
          <Trash2 className="h-4 w-4" />
          <AlertTitle>Object deleted</AlertTitle>
          <AlertDescription>
            This {kind} has been deleted from the cluster. The details below are the last known state.
          </AlertDescription>
        </Alert>
      )}

      <div className="flex gap-0.5 border-b" role="tablist" aria-label="Object views">
        <TabButton active={tab === "summary"} onClick={() => setTab("summary")}>
          Summary
        </TabButton>
        {isPod && (
          <TabButton active={tab === "logs"} onClick={() => setTab("logs")}>
            Logs
          </TabButton>
        )}
        {isPod && (
          <TabButton active={tab === "terminal"} onClick={() => setTab("terminal")}>
            Terminal
          </TabButton>
        )}
        {/* The graph is namespace-scoped (ADR-0011), so cluster-scoped objects
            have nothing to focus it on. */}
        {namespace && (
          <TabButton active={tab === "graph"} onClick={() => setTab("graph")}>
            Graph
          </TabButton>
        )}
        <TabButton active={tab === "yaml"} onClick={() => setTab("yaml")}>
          YAML
        </TabButton>
      </div>

      {tab === "logs" && isPod ? (
        <LogViewer namespace={namespace ?? ""} name={name} object={object.data} />
      ) : tab === "terminal" && isPod ? (
        // Exec is a read-ish capability from the UI's angle, but running
        // commands can mutate — the server enforces read-only, so if exec is
        // blocked the socket simply closes with an error the terminal shows.
        <ExecTerminal namespace={namespace ?? ""} name={name} object={object.data} />
      ) : tab === "graph" && namespace ? (
        <Suspense fallback={<Skeleton className="h-[420px] w-full" data-testid="graph-loading" />}>
          <ResourceGraph namespace={namespace} kind={kind} name={name} />
        </Suspense>
      ) : tab === "yaml" ? (
        // A Secret's YAML is masked, so editing it would apply the redaction
        // marker over the real data — force the YAML tab view-only for Secrets;
        // values are changed via the dedicated (revealed) flows, not here.
        <YamlTab refx={ref} kind={kind} readOnly={readOnly || isSecret} />
      ) : workload?.controller ? (
        // Controller detail resolves its own status/pods/events; the generic
        // object fetch is skipped here and only the YAML tab loads it lazily.
        <ControllerDetail
          resource={resource}
          kind={workload.kind}
          namespace={namespace ?? ""}
          name={name}
          readOnly={readOnly}
        />
      ) : object.isPending ? (
        <Skeleton className="h-40 w-full" data-testid="detail-loading" />
      ) : object.isError ? (
        <ErrorState
          error={object.error}
          onRetry={() => object.refetch()}
          title="Failed to load object"
        />
      ) : (
        <TypedDetail
          detail={detail}
          object={object.data}
          namespace={namespace}
          name={name}
          isWorkload={Boolean(workload)}
        />
      )}
    </div>
  );
}

/** The optional header status pill: pods use the derived kubectl-ish status,
 *  namespaces their phase; other kinds have no header badge. */
function deriveHeaderBadge(
  isPod: boolean,
  isNamespace: boolean,
  object: KubeObject | undefined,
): { tone: StatusTone; label: string } | null {
  if (!object) return null;
  if (isPod) {
    const label = podDisplayStatus(object as unknown as PodStatusObject);
    return { tone: podStatusTone(label), label };
  }
  if (isNamespace) {
    const phase = (object.status as { phase?: string } | undefined)?.phase ?? "Active";
    return { tone: phase === "Active" ? "ok" : "progress", label: phase };
  }
  return null;
}

/** Routes a resolved object to its typed detail view, falling back to the Pod
 *  view for the Pod kind and the generic Summary for everything unregistered
 *  (including CRDs). Rendered once the object has loaded. */
function TypedDetail({
  detail,
  object,
  namespace,
  name,
  isWorkload,
}: {
  detail: ReturnType<typeof detailKind>;
  object: KubeObject;
  namespace: string | undefined;
  name: string;
  isWorkload: boolean;
}) {
  switch (detail) {
    case "namespace":
      return <NamespaceDetail object={object} name={name} />;
    case "configmap":
      return <ConfigMapDetail object={object} />;
    case "secret":
      return <SecretDetail object={object} namespace={namespace ?? ""} name={name} />;
    case "service":
      return <ServiceDetail namespace={namespace ?? ""} name={name} />;
    case "ingress":
      return <IngressDetail object={object} namespace={namespace ?? ""} />;
    case "role":
    case "clusterrole":
      return <RoleDetail object={object} />;
    case "rolebinding":
      return <BindingDetail object={object} namespace={namespace} />;
    case "clusterrolebinding":
      return <BindingDetail object={object} namespace={undefined} />;
    case "serviceaccount":
      return <ServiceAccountDetail object={object} namespace={namespace ?? ""} />;
    case "persistentvolumeclaim":
      return <PersistentVolumeClaimDetail object={object} />;
    case "persistentvolume":
      return <PersistentVolumeDetail object={object} />;
    case "storageclass":
      return <StorageClassDetail object={object} />;
    default:
      return isWorkload ? (
        <PodDetail object={object} namespace={namespace} name={name} />
      ) : (
        <Summary object={object} />
      );
  }
}

function Summary({ object }: { object: KubeObject }) {
  const meta = object.metadata ?? {};
  const labels = Object.entries(meta.labels ?? {});
  const annotations = Object.entries(meta.annotations ?? {});
  const owners = meta.ownerReferences ?? [];

  return (
    <div className="space-y-6">
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Field label="Kind" value={object.kind ?? "—"} />
        <Field label="Namespace" value={meta.namespace ?? "—"} />
        <Field label="Age" value={formatAge(meta.creationTimestamp)} />
        <Field label="Created" value={meta.creationTimestamp ?? "—"} />
      </dl>

      <Panel title="Labels">
        {labels.length === 0 ? (
          <Empty />
        ) : (
          <div className="flex flex-wrap gap-2">
            {labels.map(([k, v]) => (
              <Badge key={k} variant="secondary" className="font-normal">
                {k}={v}
              </Badge>
            ))}
          </div>
        )}
      </Panel>

      <Panel title="Annotations">
        {annotations.length === 0 ? (
          <Empty />
        ) : (
          <dl className="space-y-1 text-sm">
            {annotations.map(([k, v]) => (
              <div key={k} className="grid grid-cols-1 gap-0.5 sm:grid-cols-[minmax(0,20rem)_1fr]">
                <dt className="truncate font-medium text-muted-foreground" title={k}>
                  {k}
                </dt>
                <dd className="break-all">{v}</dd>
              </div>
            ))}
          </dl>
        )}
      </Panel>

      <Panel title="Owner references">
        {owners.length === 0 ? (
          <Empty />
        ) : (
          <ul className="space-y-1 text-sm">
            {owners.map((o) => (
              <li key={o.uid ?? `${o.kind}/${o.name}`} className="flex items-center gap-2">
                <span className="font-medium">
                  {o.kind}/{o.name}
                </span>
                {o.controller && (
                  <Badge variant="outline" className="font-normal">
                    controller
                  </Badge>
                )}
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-all text-sm font-medium">{value}</dd>
    </div>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold">{title}</h3>
      {children}
    </section>
  );
}

function Empty() {
  return <p className="text-sm text-muted-foreground">None</p>;
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "-mb-px border-b-2 px-3 py-1.5 text-[13px] transition-colors",
        active
          ? "border-primary font-medium text-foreground"
          : "border-transparent text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

