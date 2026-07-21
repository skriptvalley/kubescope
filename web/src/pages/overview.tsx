import { AlertTriangle, RefreshCw } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { ErrorState } from "@/components/error-state";
import { LiveBadge } from "@/components/live-badge";
import { PodsTable } from "@/components/pods-table";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useNodes } from "@/hooks/use-nodes";
import { useOverview } from "@/hooks/use-overview";
import { usePodMetrics } from "@/hooks/use-metrics";
import { useLiveWorkloadSummary } from "@/hooks/use-stream";
import type { PodSummary } from "@/lib/api";
import { toneStyles } from "@/lib/tone-style";
import { cn } from "@/lib/utils";
import { podStatusTone, type StatusTone } from "@/lib/workload-status";

const POD_GVR = { group: "core", version: "v1", resource: "pods" };
const STATUS_OPTIONS = ["Running", "Pending", "CrashLoopBackOff", "Completed"];

export function OverviewPage() {
  const overview = useOverview();
  const pods = useLiveWorkloadSummary<PodSummary>("pods", POD_GVR);
  const nodes = useNodes();
  const metrics = usePodMetrics();
  const navigate = useNavigate();

  const [nsFilter, setNsFilter] = useState("all");
  const [statusFilter, setStatusFilter] = useState("all");
  const [nameFilter, setNameFilter] = useState("");

  const allPods = useMemo(() => pods.data ?? [], [pods.data]);
  const running = allPods.filter((p) => p.status === "Running").length;
  const pending = allPods.filter((p) => podStatusTone(p.status) === "progress").length;
  const failing = allPods.filter((p) => podStatusTone(p.status) === "warn").length;
  const firstFailing = allPods.find((p) => podStatusTone(p.status) === "warn");

  const readyNodes = (nodes.data ?? []).filter((n) => n.status === "Ready").length;
  const nsList = overview.data?.namespaces ?? [];
  const systemNs = nsList.filter((n) => n.startsWith("kube-")).length;

  const filtered = useMemo(() => {
    const name = nameFilter.trim();
    return allPods.filter(
      (p) =>
        (nsFilter === "all" || p.namespace === nsFilter) &&
        (statusFilter === "all" || p.status === statusFilter) &&
        (name === "" || p.name.includes(name)),
    );
  }, [allPods, nsFilter, statusFilter, nameFilter]);

  const refreshAll = () => {
    void overview.refetch();
    void pods.refetch();
    void nodes.refetch();
    void metrics.query.refetch();
  };

  if (overview.isError) {
    return (
      <ErrorState error={overview.error} onRetry={() => overview.refetch()} title="Cluster unreachable" />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-display text-xl font-semibold tracking-[-0.02em]">Cluster overview</h1>
          <p className="mt-0.5 flex flex-wrap items-center gap-2 text-[13px] text-muted-foreground">
            <span className="font-mono text-xs">{overview.data?.context ?? "…"}</span>
            {overview.data?.serverVersion && (
              <>
                <span>·</span>
                <span>{overview.data.serverVersion}</span>
              </>
            )}
            <span>·</span>
            <LiveBadge status={pods.streamStatus} />
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={refreshAll} aria-label="Refresh overview">
          <RefreshCw className={overview.isFetching || pods.isFetching ? "animate-spin" : undefined} />
          Refresh
        </Button>
      </div>

      {failing > 0 && firstFailing && (
        <div
          role="status"
          className="flex items-center gap-2.5 rounded-lg border border-destructive/25 bg-destructive/[0.07] px-3 py-2 text-[13px]"
        >
          <AlertTriangle className="h-[15px] w-[15px] shrink-0 text-destructive" />
          <span className="min-w-0 flex-1">
            <span className="font-medium text-destructive">
              {failing} workload{failing > 1 ? "s" : ""} failing
            </span>{" "}
            — <span className="font-mono text-xs">{firstFailing.name}</span> is in {firstFailing.status} in{" "}
            <span className="font-mono text-xs">{firstFailing.namespace}</span>
          </span>
          <button
            type="button"
            onClick={() =>
              navigate(
                `/resources/core/v1/pods/${encodeURIComponent(firstFailing.namespace)}/${encodeURIComponent(firstFailing.name)}`,
              )
            }
            className="inline-flex h-6 shrink-0 items-center rounded-sm bg-destructive/10 px-2.5 text-xs font-medium text-destructive hover:bg-destructive/20"
          >
            Inspect
          </button>
        </div>
      )}

      <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(185px, 1fr))" }}>
        <StatCard label="Nodes" value={String(overview.data?.nodeCount ?? "—")}>
          {nodes.data && <DotBadge tone="ok" label={`${readyNodes} Ready`} />}
        </StatCard>
        <StatCard label="Pods" value={pods.isPending ? "—" : String(allPods.length)}>
          <DotBadge tone="ok" label={`${running} Running`} />
          {pending > 0 && <DotBadge tone="progress" label={`${pending} Pending`} />}
          {failing > 0 && <DotBadge tone="warn" label={`${failing} Failing`} />}
        </StatCard>
        <StatCard label="Namespaces" value={String(nsList.length)}>
          {systemNs > 0 && <DotBadge tone="neutral" label={`${systemNs} system`} />}
        </StatCard>
        <StatCard
          label="Health"
          value={pods.isPending ? "…" : failing > 0 ? "Degraded" : "Healthy"}
          valueClass={failing > 0 ? "text-destructive" : undefined}
        >
          {!pods.isPending &&
            (failing > 0 ? (
              <DotBadge tone="warn" label={`${failing} failing`} />
            ) : (
              <DotBadge tone="ok" label="All workloads healthy" />
            ))}
        </StatCard>
      </div>

      <div className="overflow-hidden rounded-lg bg-card shadow-ring">
        <div className="flex flex-wrap items-center gap-2.5 px-4 pb-2.5 pt-3">
          <div className="flex items-baseline gap-2">
            <h2 className="font-display text-[15px] font-medium">Pods</h2>
            <span className="font-mono text-xs text-muted-foreground">
              {filtered.length} of {allPods.length}
            </span>
          </div>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <FilterSelect value={nsFilter} onChange={setNsFilter} aria-label="Filter by namespace">
              <option value="all">All namespaces</option>
              {nsList.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </FilterSelect>
            <FilterSelect value={statusFilter} onChange={setStatusFilter} aria-label="Filter by status">
              <option value="all">All statuses</option>
              {STATUS_OPTIONS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </FilterSelect>
            <input
              value={nameFilter}
              onChange={(e) => setNameFilter(e.target.value)}
              placeholder="Filter by name…"
              aria-label="Filter pods by name"
              className="h-7 w-[170px] rounded-md border border-input bg-transparent px-2.5 text-[12.5px] outline-none transition-[box-shadow,border-color] focus:border-ring focus:ring-[3px] focus:ring-ring/40"
            />
          </div>
        </div>
        {pods.isPending ? (
          <div className="space-y-2 p-4" data-testid="overview-pods-loading">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : pods.isError ? (
          <div className="p-4">
            <ErrorState error={pods.error} onRetry={() => pods.refetch()} title="Failed to load pods" />
          </div>
        ) : (
          <PodsTable
            pods={filtered}
            metrics={metrics.byPod}
            showNamespace
            showNode
            showCpuMem
            emptyMessage={allPods.length === 0 ? "No pods in this cluster." : "No pods match the filters."}
          />
        )}
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  valueClass,
  children,
}: {
  label: string;
  value: string;
  valueClass?: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg bg-card p-3.5 shadow-ring">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p
        className={cn(
          "font-display text-[26px] font-semibold leading-none tracking-[-0.02em]",
          valueClass,
        )}
      >
        {value}
      </p>
      <div className="flex min-h-5 flex-wrap items-center gap-1">{children}</div>
    </div>
  );
}

function DotBadge({ tone, label }: { tone: StatusTone; label: string }) {
  const s = toneStyles[tone];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-[11.5px] font-medium",
        s.pill,
      )}
    >
      <span className={cn("h-[5px] w-[5px] rounded-full", s.dot)} aria-hidden="true" />
      {label}
    </span>
  );
}

function FilterSelect({
  value,
  onChange,
  children,
  ...props
}: {
  value: string;
  onChange: (v: string) => void;
  children: React.ReactNode;
} & Omit<React.SelectHTMLAttributes<HTMLSelectElement>, "value" | "onChange">) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="h-7 rounded-md border border-input bg-card px-2 text-[12.5px] text-foreground outline-none transition-[box-shadow,border-color] focus:border-ring focus:ring-[3px] focus:ring-ring/40"
      {...props}
    >
      {children}
    </select>
  );
}
