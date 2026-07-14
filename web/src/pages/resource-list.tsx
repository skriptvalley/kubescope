import { AlertCircle, RefreshCw } from "lucide-react";
import { useCallback } from "react";
import { useParams, useSearchParams } from "react-router-dom";

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
import { useResourceList } from "@/hooks/use-resource";
import { ApiError, type ResourceRow } from "@/lib/api";
import { findResource } from "@/lib/discovery-nav";

export function ResourceListPage() {
  const params = useParams();
  const group = params.group ?? "";
  const version = params.version ?? "";
  const resource = params.resource ?? "";

  const [searchParams, setSearchParams] = useSearchParams();
  const ns = searchParams.get("ns") ?? "";

  const { data: discovery } = useDiscovery();
  const info = findResource(discovery, { group, version, resource });
  const namespaced = info?.namespaced ?? false;
  const title = info?.kind ?? resource;

  // Only namespaced kinds carry a namespace scope; cluster-scoped lists never
  // send one (the backend rejects it).
  const effectiveNamespace = namespaced && ns ? ns : undefined;
  const list = useResourceList({ group, version, resource, namespace: effectiveNamespace });
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
