import { RefreshCw } from "lucide-react";

import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useOverview } from "@/hooks/use-overview";

export function OverviewPage() {
  const { data, isPending, isError, error, refetch, isFetching } = useOverview();

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div className="space-y-1.5">
          <CardTitle>Cluster overview</CardTitle>
          <CardDescription>
            {data ? `Context ${data.context}` : "Active kubeconfig context"}
          </CardDescription>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={isFetching}
          aria-label="Refresh overview"
        >
          <RefreshCw className={isFetching ? "animate-spin" : undefined} />
          Refresh
        </Button>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <OverviewSkeleton />
        ) : isError ? (
          <ErrorState error={error} onRetry={() => refetch()} title="Cluster unreachable" />
        ) : (
          <div className="space-y-6">
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
              <Stat label="Server version" value={data.serverVersion || "—"} />
              <Stat label="Nodes" value={String(data.nodeCount)} />
              <Stat label="Namespaces" value={String(data.namespaces.length)} />
            </div>
            <div className="space-y-2">
              <p className="text-sm font-medium">Namespaces</p>
              {data.namespaces.length === 0 ? (
                <EmptyState message="No namespaces found." />
              ) : (
                <div className="flex flex-wrap gap-2">
                  {data.namespaces.map((ns) => (
                    <Badge key={ns} variant="secondary">
                      {ns}
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border p-4">
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold tracking-tight">{value}</p>
    </div>
  );
}

function OverviewSkeleton() {
  return (
    <div className="space-y-6" data-testid="overview-loading">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="h-20 w-full" />
        ))}
      </div>
      <Skeleton className="h-8 w-full" />
    </div>
  );
}

