import { AlertCircle, RefreshCw } from "lucide-react";
import { useCallback } from "react";
import { useParams, useSearchParams } from "react-router-dom";

import { LiveBadge } from "@/components/live-badge";
import { NamespaceSelector } from "@/components/namespace-selector";
import { ResourceTable } from "@/components/resource-table";
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
import { useDiscovery } from "@/hooks/use-discovery";
import { useNamespaces } from "@/hooks/use-namespaces";
import { useLiveResourceList } from "@/hooks/use-stream";
import { ApiError, type ResourceRow } from "@/lib/api";
import { findResource } from "@/lib/discovery-nav";
import { workloadKind } from "@/lib/workloads";
import { WorkloadListPage } from "@/pages/workload-list";

/** Route dispatcher: the seven workload kinds get the typed summary view; the
 *  generic discovery+dynamic engine serves everything else (ADR-0003). The
 *  branch renders distinct component types, so switching kinds remounts cleanly
 *  and the two paths never share a hook order. */
export function ResourceListPage() {
  const params = useParams();
  const ref = {
    group: params.group ?? "",
    version: params.version ?? "",
    resource: params.resource ?? "",
  };
  const workload = workloadKind(ref);
  if (workload) {
    return <WorkloadListPage key={ref.resource} {...ref} kind={workload.kind} />;
  }
  return <GenericResourceListPage />;
}

function GenericResourceListPage() {
  const params = useParams();
  const group = params.group ?? "";
  const version = params.version ?? "";
  const resource = params.resource ?? "";

  const [searchParams, setSearchParams] = useSearchParams();
  const ns = searchParams.get("ns") ?? "";

  const { data: discovery } = useDiscovery();
  const info = findResource(discovery, { group, version, resource });
  const discoveryNamespaced = info?.namespaced; // undefined until discovery resolves
  const title = info?.kind ?? resource;

  // List scope: send the selected namespace unless discovery has positively told
  // us the kind is cluster-scoped. A namespaced deep-link (…?ns=x) then lists the
  // right namespace on the FIRST request instead of briefly listing all
  // namespaces while discovery loads; cluster-scoped lists still never send one.
  const effectiveNamespace = discoveryNamespaced === false ? undefined : ns || undefined;
  const list = useLiveResourceList({ group, version, resource, namespace: effectiveNamespace });
  const namespaces = useNamespaces();

  // The list response's `namespaced` is authoritative and same-response; prefer
  // it for the selector and detail links, falling back to discovery pre-load.
  const namespaced = list.data?.namespaced ?? discoveryNamespaced ?? false;

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

  const detailHref = useCallback(
    (row: ResourceRow) => {
      const base = `/resources/${group}/${version}/${resource}`;
      return namespaced && row.namespace
        ? `${base}/${encodeURIComponent(row.namespace)}/${encodeURIComponent(row.name)}`
        : `${base}/${encodeURIComponent(row.name)}`;
    },
    [group, version, resource, namespaced],
  );

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div className="space-y-1.5">
          <CardTitle>{title}</CardTitle>
          <CardDescription>
            {group === "core" ? "core" : group}/{version} · {resource}
          </CardDescription>
        </div>
        <div className="flex items-center gap-3">
          <LiveBadge status={list.streamStatus} />
          {namespaced && (
            <NamespaceSelector
              value={ns}
              onChange={setNamespace}
              namespaces={namespaces.data ?? []}
              isLoading={namespaces.isPending}
            />
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={() => list.refetch()}
            disabled={list.isFetching}
            aria-label="Refresh list"
          >
            <RefreshCw className={list.isFetching ? "animate-spin" : undefined} />
            Refresh
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {list.isPending ? (
          <ListSkeleton />
        ) : list.isError ? (
          <ListError error={list.error} />
        ) : list.data.rows.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No {resource} found{namespaced && ns ? ` in namespace ${ns}` : ""}.
          </p>
        ) : (
          <ResourceTable columns={list.data.columns} rows={list.data.rows} detailHref={detailHref} />
        )}
      </CardContent>
    </Card>
  );
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
    apiError?.code === "unknown_resource"
      ? "Unknown resource"
      : apiError?.code === "invalid_scope"
        ? "Invalid namespace scope"
        : "Failed to load resources";
  const detail = apiError ? `${apiError.message} (${apiError.code})` : error.message;
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{detail}</AlertDescription>
    </Alert>
  );
}
