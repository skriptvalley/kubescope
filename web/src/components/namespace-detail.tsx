import { DetailField, DetailGrid, DetailSection, LabelBadges } from "@/components/detail-ui";
import { PodsTable } from "@/components/pods-table";
import { ErrorState } from "@/components/error-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useNamespaceQuotas } from "@/hooks/use-quotas";
import { useLiveWorkloadSummary } from "@/hooks/use-stream";
import type { KubeObject, PodSummary } from "@/lib/api";
import { formatAge } from "@/lib/age";
import { podStatusTone } from "@/lib/workload-status";

const POD_GVR = { group: "core", version: "v1", resource: "pods" };

/** Dusk Namespace detail: fields, labels, ResourceQuota bars (Story F) and the
 *  live pods-in-namespace table. Layered on the generic detail page — the
 *  breadcrumb, header badge and Delete live in the parent. */
export function NamespaceDetail({ object, name }: { object: KubeObject; name: string }) {
  const pods = useLiveWorkloadSummary<PodSummary>("pods", POD_GVR, name);
  const quotas = useNamespaceQuotas(name);

  const meta = object.metadata ?? {};
  const phase = (object.status as { phase?: string } | undefined)?.phase ?? "Active";
  const list = pods.data ?? [];
  const running = list.filter((p) => p.status === "Running").length;
  const failing = list.filter((p) => podStatusTone(p.status) === "warn").length;
  const podsField = pods.isPending
    ? "…"
    : list.length === 0
      ? "0"
      : `${list.length} — ${running} running${failing ? `, ${failing} failing` : ""}`;

  const quotaItems = quotas.data ?? [];

  return (
    <div className="flex flex-col gap-[18px]">
      <DetailGrid>
        <DetailField label="Status" value={phase} />
        <DetailField label="Age" value={formatAge(meta.creationTimestamp)} />
        <DetailField label="Pods" value={podsField} />
      </DetailGrid>

      <DetailSection title="Labels">
        <LabelBadges pairs={meta.labels} />
      </DetailSection>

      {quotaItems.length > 0 && (
        <DetailSection title="Resource quota">
          <div
            className="grid gap-3"
            style={{ gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))" }}
          >
            {quotaItems.map((q) => (
              <div key={`${q.quotaName}/${q.resource}`} className="rounded-lg bg-card p-3.5 shadow-ring">
                <div className="mb-2 flex items-baseline justify-between gap-2">
                  <span className="truncate font-mono text-xs font-medium text-muted-foreground">
                    {q.resource}
                  </span>
                  <span className="shrink-0 font-mono text-xs">
                    {q.used} <span className="text-muted-foreground">/ {q.hard}</span>
                  </span>
                </div>
                <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                  <div className="h-full rounded-full bg-chart-1" style={{ width: `${q.percent}%` }} />
                </div>
              </div>
            ))}
          </div>
        </DetailSection>
      )}

      <DetailSection title="Pods in this namespace">
        {pods.isPending ? (
          <div className="space-y-2" data-testid="ns-pods-loading">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : pods.isError ? (
          <ErrorState error={pods.error} onRetry={() => pods.refetch()} title="Failed to load pods" />
        ) : (
          <div className="overflow-hidden rounded-lg bg-card shadow-ring">
            <PodsTable pods={list} emptyMessage={`No pods in ${name}.`} />
          </div>
        )}
      </DetailSection>
    </div>
  );
}
