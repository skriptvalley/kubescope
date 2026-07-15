import { type ColumnDef } from "@tanstack/react-table";
import { AlertCircle, RefreshCw } from "lucide-react";
import { useCallback } from "react";
import { Link, useSearchParams } from "react-router-dom";

import { LiveBadge } from "@/components/live-badge";
import { NamespaceSelector } from "@/components/namespace-selector";
import { DeleteRowButton } from "@/components/resource-actions";
import { StatusBadge } from "@/components/status-badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { WorkloadTable } from "@/components/workload-table";
import { useReadOnly } from "@/hooks/use-config";
import { useNamespaces } from "@/hooks/use-namespaces";
import { useLiveWorkloadSummary } from "@/hooks/use-stream";
import { formatAge } from "@/lib/age";
import {
  ApiError,
  type CronJobSummary,
  type DaemonSetSummary,
  type DeploymentSummary,
  type JobSummary,
  type PodSummary,
  type ReplicaSetSummary,
  type StatefulSetSummary,
  type WorkloadSummary,
} from "@/lib/api";
import { podStatusTone } from "@/lib/workload-status";

/** Typed workload list page: one hook fetches the kind's summary rows, a pure
 *  column selection renders kind-specific columns. Rendered by ResourceListPage
 *  for the seven workload kinds; the generic engine serves everything else. */
export function WorkloadListPage({
  group,
  version,
  resource,
  kind,
}: {
  group: string;
  version: string;
  resource: string;
  kind: string;
}) {
  const [searchParams, setSearchParams] = useSearchParams();
  const ns = searchParams.get("ns") ?? "";
  const readOnly = useReadOnly();

  // All seven workload kinds are namespaced; an empty selection lists all.
  const query = useLiveWorkloadSummary<WorkloadSummary>(
    resource,
    { group, version, resource },
    ns || undefined,
  );
  const namespaces = useNamespaces();

  const setNamespace = useCallback(
    (value: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) next.set("ns", value);
          else next.delete("ns");
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const detailBase = `/resources/${group}/${version}/${resource}`;
  const columns = columnsFor(resource, detailBase);

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div className="space-y-1.5">
          <CardTitle>{kind}</CardTitle>
          <CardDescription>
            {group === "core" ? "core" : group}/{version} · {resource}
          </CardDescription>
        </div>
        <div className="flex items-center gap-3">
          <LiveBadge status={query.streamStatus} />
          <NamespaceSelector
            value={ns}
            onChange={setNamespace}
            namespaces={namespaces.data ?? []}
            isLoading={namespaces.isPending}
          />
          <Button
            variant="outline"
            size="sm"
            onClick={() => query.refetch()}
            disabled={query.isFetching}
            aria-label="Refresh list"
          >
            <RefreshCw className={query.isFetching ? "animate-spin" : undefined} />
            Refresh
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {query.isPending ? (
          <ListSkeleton />
        ) : query.isError ? (
          <ListError error={query.error} />
        ) : query.data.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No {resource} found{ns ? ` in namespace ${ns}` : ""}.
          </p>
        ) : (
          <WorkloadTable
            columns={columns}
            rows={query.data}
            rowAction={
              readOnly
                ? undefined
                : (row) => (
                    <DeleteRowButton
                      refx={{ group, version, resource, namespace: row.namespace, name: row.name }}
                      kind={kind}
                    />
                  )
            }
          />
        )}
      </CardContent>
    </Card>
  );
}

// --- Column definitions (pure) -----------------------------------------------

/** The name column links to the object's detail route. All workloads are
 *  namespaced, so the link always carries the namespace segment. */
function nameColumn(detailBase: string): ColumnDef<WorkloadSummary> {
  return {
    id: "name",
    header: "Name",
    accessorFn: (row) => row.name,
    cell: ({ row }) => (
      <Link
        to={`${detailBase}/${encodeURIComponent(row.original.namespace)}/${encodeURIComponent(row.original.name)}`}
        className="font-medium text-foreground underline-offset-4 hover:underline"
      >
        {row.original.name}
      </Link>
    ),
  };
}

function namespaceColumn(): ColumnDef<WorkloadSummary> {
  return {
    id: "namespace",
    header: "Namespace",
    accessorFn: (row) => row.namespace,
    cell: ({ row }) => <span className="text-muted-foreground">{row.original.namespace}</span>,
  };
}

function ageColumn(): ColumnDef<WorkloadSummary> {
  return {
    id: "age",
    header: "Age",
    accessorFn: (row) => (row.creationTimestamp ? Date.parse(row.creationTimestamp) : 0),
    sortingFn: "basic",
    cell: ({ row }) => (
      <span className="text-muted-foreground">{formatAge(row.original.creationTimestamp)}</span>
    ),
  };
}

function text(header: string, get: (row: WorkloadSummary) => string): ColumnDef<WorkloadSummary> {
  return {
    id: header.toLowerCase().replace(/\s+/g, "-"),
    header,
    accessorFn: get,
    cell: ({ getValue }) => <span>{String(getValue())}</span>,
  };
}

function columnsFor(resource: string, detailBase: string): ColumnDef<WorkloadSummary>[] {
  const name = nameColumn(detailBase);
  const namespace = namespaceColumn();
  const age = ageColumn();

  switch (resource) {
    case "pods":
      return [
        name,
        namespace,
        text("Ready", (r) => (r as PodSummary).ready),
        {
          id: "status",
          header: "Status",
          accessorFn: (r) => (r as PodSummary).status,
          cell: ({ row }) => {
            const p = row.original as PodSummary;
            return <StatusBadge tone={podStatusTone(p.status)}>{p.status}</StatusBadge>;
          },
        },
        {
          id: "restarts",
          header: "Restarts",
          accessorFn: (r) => (r as PodSummary).restarts,
          sortingFn: "basic",
          cell: ({ getValue }) => <span>{String(getValue())}</span>,
        },
        text("Node", (r) => (r as PodSummary).node ?? "—"),
        age,
      ];
    case "deployments":
      return [
        name,
        namespace,
        text("Ready", (r) => (r as DeploymentSummary).ready),
        text("Up-to-date", (r) => String((r as DeploymentSummary).updatedReplicas)),
        text("Available", (r) => String((r as DeploymentSummary).availableReplicas)),
        age,
      ];
    case "statefulsets":
      return [
        name,
        namespace,
        text("Ready", (r) => (r as StatefulSetSummary).ready),
        text("Updated", (r) => String((r as StatefulSetSummary).updatedReplicas)),
        age,
      ];
    case "daemonsets":
      return [
        name,
        namespace,
        text("Desired", (r) => String((r as DaemonSetSummary).desired)),
        text("Ready", (r) => String((r as DaemonSetSummary).ready)),
        text("Up-to-date", (r) => String((r as DaemonSetSummary).upToDate)),
        text("Available", (r) => String((r as DaemonSetSummary).available)),
        age,
      ];
    case "replicasets":
      return [
        name,
        namespace,
        text("Ready", (r) => (r as ReplicaSetSummary).ready),
        text("Current", (r) => String((r as ReplicaSetSummary).currentReplicas)),
        age,
      ];
    case "jobs":
      return [
        name,
        namespace,
        text("Completions", (r) => (r as JobSummary).completions),
        text("Duration", (r) => (r as JobSummary).duration ?? "—"),
        age,
      ];
    case "cronjobs":
      return [
        name,
        namespace,
        text("Schedule", (r) => (r as CronJobSummary).schedule),
        text("Suspend", (r) => ((r as CronJobSummary).suspend ? "True" : "False")),
        text("Active", (r) => String((r as CronJobSummary).active)),
        {
          id: "last-schedule",
          header: "Last schedule",
          accessorFn: (r) => (r as CronJobSummary).lastScheduleTime ?? "",
          cell: ({ row }) => (
            <span className="text-muted-foreground">
              {formatAge((row.original as CronJobSummary).lastScheduleTime)}
            </span>
          ),
        },
        age,
      ];
    default:
      return [name, namespace, age];
  }
}

function ListSkeleton() {
  return (
    <div className="space-y-2" data-testid="resource-list-loading">
      {[0, 1, 2, 3, 4].map((i) => (
        <Skeleton key={i} className="h-9 w-full" />
      ))}
    </div>
  );
}

function ListError({ error }: { error: Error }) {
  const apiError = error instanceof ApiError ? error : undefined;
  const title =
    apiError?.code === "unknown_workload" ? "Unknown workload" : "Failed to load workloads";
  const detail = apiError ? `${apiError.message} (${apiError.code})` : error.message;
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{detail}</AlertDescription>
    </Alert>
  );
}
