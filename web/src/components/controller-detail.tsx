import { type ColumnDef } from "@tanstack/react-table";
import { AlertCircle } from "lucide-react";
import { Link } from "react-router-dom";

import { EventsPanel } from "@/components/events-panel";
import { StatusBadge } from "@/components/status-badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { WorkloadTable } from "@/components/workload-table";
import { useOwnedJobs, useOwnedPods, useWorkloadSummary } from "@/hooks/use-workloads";
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
import { routeForKind } from "@/lib/workloads";
import { podStatusTone } from "@/lib/workload-status";

const POD_OWNING = new Set(["deployments", "statefulsets", "daemonsets", "replicasets", "jobs"]);

/** Controller detail: replica health + a kubectl-style rollout line (both
 *  computed server-side), plus the pods (or, for a CronJob, the Jobs) it owns.
 *  Rendered by ResourceDetailPage for the six controller kinds; the Pod kind
 *  has its own view. The raw YAML tab lives on the parent. */
export function ControllerDetail({
  resource,
  kind,
  namespace,
  name,
}: {
  resource: string;
  kind: string;
  namespace: string;
  name: string;
}) {
  // All hooks run unconditionally (enabled toggles the fetch) so switching
  // between controller kinds without unmount never trips the rules of hooks.
  const summary = useWorkloadSummary<WorkloadSummary>(resource, namespace);
  const ownedPods = useOwnedPods(resource, namespace, name, POD_OWNING.has(resource));
  const ownedJobs = useOwnedJobs(namespace, name, resource === "cronjobs");

  const row = summary.data?.find((r) => r.name === name);

  return (
    <div className="space-y-6">
      {summary.isPending ? (
        <Skeleton className="h-20 w-full" data-testid="controller-summary-loading" />
      ) : summary.isError ? (
        <LoadError what="status" error={summary.error} />
      ) : row ? (
        <ReplicaStatus resource={resource} row={row} namespace={namespace} />
      ) : (
        <p className="text-sm text-muted-foreground">No live status for {name}.</p>
      )}

      {resource === "cronjobs" ? (
        <section className="space-y-2" aria-label="Jobs">
          <h3 className="text-sm font-semibold">Jobs</h3>
          <OwnedJobsTable query={ownedJobs} namespace={namespace} />
        </section>
      ) : (
        <section className="space-y-2" aria-label="Pods">
          <h3 className="text-sm font-semibold">Pods</h3>
          <OwnedPodsTable query={ownedPods} />
        </section>
      )}

      <EventsPanel kind={kind} namespace={namespace} name={name} />
    </div>
  );
}

function ReplicaStatus({
  resource,
  row,
  namespace,
}: {
  resource: string;
  row: WorkloadSummary;
  namespace: string;
}) {
  switch (resource) {
    case "deployments": {
      const d = row as DeploymentSummary;
      return (
        <StatusBlock rollout={d.rolloutStatus}>
          <Stat label="Ready" value={d.ready} />
          <Stat label="Up-to-date" value={String(d.updatedReplicas)} />
          <Stat label="Available" value={String(d.availableReplicas)} />
          <Stat label="Desired" value={String(d.desiredReplicas)} />
        </StatusBlock>
      );
    }
    case "statefulsets": {
      const s = row as StatefulSetSummary;
      return (
        <StatusBlock rollout={s.rolloutStatus}>
          <Stat label="Ready" value={s.ready} />
          <Stat label="Current" value={String(s.currentReplicas)} />
          <Stat label="Updated" value={String(s.updatedReplicas)} />
          <Stat label="Desired" value={String(s.desiredReplicas)} />
        </StatusBlock>
      );
    }
    case "daemonsets": {
      const d = row as DaemonSetSummary;
      return (
        <StatusBlock rollout={d.rolloutStatus}>
          <Stat label="Desired" value={String(d.desired)} />
          <Stat label="Current" value={String(d.current)} />
          <Stat label="Ready" value={String(d.ready)} />
          <Stat label="Up-to-date" value={String(d.upToDate)} />
          <Stat label="Available" value={String(d.available)} />
        </StatusBlock>
      );
    }
    case "replicasets": {
      const rs = row as ReplicaSetSummary;
      const ownerRoute = rs.owner ? routeForKind(rs.owner.kind, namespace, rs.owner.name) : undefined;
      return (
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Stat label="Ready" value={rs.ready} />
            <Stat label="Current" value={String(rs.currentReplicas)} />
            <Stat label="Desired" value={String(rs.desiredReplicas)} />
          </div>
          <p className="text-sm">
            <span className="text-muted-foreground">Owned by: </span>
            {rs.owner ? (
              ownerRoute ? (
                <Link to={ownerRoute} className="font-medium underline-offset-4 hover:underline">
                  {rs.owner.kind}/{rs.owner.name}
                </Link>
              ) : (
                <span className="font-medium">
                  {rs.owner.kind}/{rs.owner.name}
                </span>
              )
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </p>
        </div>
      );
    }
    case "jobs": {
      const j = row as JobSummary;
      return (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Stat label="Completions" value={j.completions} />
          <Stat label="Succeeded" value={String(j.succeeded)} />
          <Stat label="Failed" value={String(j.failed)} />
          <Stat label="Active" value={String(j.active)} />
          {j.duration && <Stat label="Duration" value={j.duration} />}
        </div>
      );
    }
    case "cronjobs": {
      const c = row as CronJobSummary;
      return (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <Stat label="Schedule" value={c.schedule} />
          <Stat label="Suspended" value={c.suspend ? "Yes" : "No"} />
          <Stat label="Active jobs" value={String(c.active)} />
          <Stat label="Last schedule" value={formatAge(c.lastScheduleTime)} />
        </div>
      );
    }
    default:
      return null;
  }
}

function StatusBlock({ rollout, children }: { rollout: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">{children}</div>
      <p className="text-sm" data-testid="rollout-status">
        <span className="text-muted-foreground">Rollout: </span>
        <span className="font-medium">{rollout}</span>
      </p>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-lg font-semibold tracking-tight">{value}</p>
    </div>
  );
}

function OwnedPodsTable({ query }: { query: ReturnType<typeof useOwnedPods> }) {
  if (query.isPending) return <Skeleton className="h-16 w-full" data-testid="owned-pods-loading" />;
  if (query.isError) return <LoadError what="pods" error={query.error} />;
  if (query.data.length === 0)
    return <p className="text-sm text-muted-foreground">No pods owned by this controller.</p>;
  return <WorkloadTable<WorkloadSummary> columns={ownedPodColumns()} rows={query.data} />;
}

function OwnedJobsTable({
  query,
  namespace,
}: {
  query: ReturnType<typeof useOwnedJobs>;
  namespace: string;
}) {
  if (query.isPending) return <Skeleton className="h-16 w-full" data-testid="owned-jobs-loading" />;
  if (query.isError) return <LoadError what="jobs" error={query.error} />;
  if (query.data.length === 0)
    return <p className="text-sm text-muted-foreground">No jobs for this cron job yet.</p>;
  return <WorkloadTable<WorkloadSummary> columns={ownedJobColumns(namespace)} rows={query.data} />;
}

function ownedPodColumns(): ColumnDef<WorkloadSummary>[] {
  return [
    {
      id: "name",
      header: "Pod",
      accessorFn: (row) => row.name,
      cell: ({ row }) => {
        const p = row.original as PodSummary;
        return (
          <Link
            to={`/resources/core/v1/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}`}
            className="font-medium underline-offset-4 hover:underline"
          >
            {p.name}
          </Link>
        );
      },
    },
    { id: "ready", header: "Ready", accessorFn: (row) => (row as PodSummary).ready },
    {
      id: "status",
      header: "Status",
      accessorFn: (row) => (row as PodSummary).status,
      cell: ({ row }) => {
        const p = row.original as PodSummary;
        return <StatusBadge tone={podStatusTone(p.status)}>{p.status}</StatusBadge>;
      },
    },
    {
      id: "restarts",
      header: "Restarts",
      accessorFn: (row) => (row as PodSummary).restarts,
      sortingFn: "basic",
      cell: ({ getValue }) => <span>{String(getValue())}</span>,
    },
    {
      id: "age",
      header: "Age",
      accessorFn: (row) => (row.creationTimestamp ? Date.parse(row.creationTimestamp) : 0),
      sortingFn: "basic",
      cell: ({ row }) => (
        <span className="text-muted-foreground">{formatAge(row.original.creationTimestamp)}</span>
      ),
    },
  ];
}

function ownedJobColumns(namespace: string): ColumnDef<WorkloadSummary>[] {
  return [
    {
      id: "name",
      header: "Job",
      accessorFn: (row) => row.name,
      cell: ({ row }) => (
        <Link
          to={`/resources/batch/v1/jobs/${encodeURIComponent(namespace)}/${encodeURIComponent(row.original.name)}`}
          className="font-medium underline-offset-4 hover:underline"
        >
          {row.original.name}
        </Link>
      ),
    },
    { id: "completions", header: "Completions", accessorFn: (row) => (row as JobSummary).completions },
    { id: "duration", header: "Duration", accessorFn: (row) => (row as JobSummary).duration ?? "—" },
    {
      id: "age",
      header: "Age",
      accessorFn: (row) => (row.creationTimestamp ? Date.parse(row.creationTimestamp) : 0),
      sortingFn: "basic",
      cell: ({ row }) => (
        <span className="text-muted-foreground">{formatAge(row.original.creationTimestamp)}</span>
      ),
    },
  ];
}

function LoadError({ what, error }: { what: string; error: Error }) {
  const apiError = error instanceof ApiError ? error : undefined;
  const detail = apiError ? `${apiError.message} (${apiError.code})` : error.message;
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>Failed to load {what}</AlertTitle>
      <AlertDescription>{detail}</AlertDescription>
    </Alert>
  );
}
