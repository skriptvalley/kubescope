import { useCallback } from "react";
import { Link, useSearchParams } from "react-router-dom";

import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { LiveBadge } from "@/components/live-badge";
import { NamespaceSelector } from "@/components/namespace-selector";
import { StatusBadge } from "@/components/status-badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useNamespaces } from "@/hooks/use-namespaces";
import { useEventsFeed } from "@/hooks/use-stream";
import { formatAge } from "@/lib/age";
import { type EventFeedRow } from "@/lib/api";
import { eventTypeTone } from "@/lib/workload-status";
import { routeForKind } from "@/lib/workloads";

type TypeFilter = "all" | "Normal" | "Warning";

/** Live cluster-wide (or per-namespace) events feed (Story 4.4): filterable by
 *  namespace and type, updating in place via the watch→SSE bridge. */
export function EventsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const ns = searchParams.get("ns") ?? "";
  const typeFilter = (searchParams.get("type") as TypeFilter) || "all";

  const feed = useEventsFeed(ns || undefined);
  const namespaces = useNamespaces();

  const setParam = useCallback(
    (name: string, value: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) next.set(name, value);
          else next.delete(name);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const rows = (feed.data ?? []).filter((e) => typeFilter === "all" || e.type === typeFilter);

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div className="space-y-1.5">
          <CardTitle>Events</CardTitle>
          <CardDescription>Cluster events, newest first</CardDescription>
        </div>
        <div className="flex items-center gap-3">
          <LiveBadge status={feed.streamStatus} />
          <label className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">Type</span>
            <select
              aria-label="Event type"
              value={typeFilter}
              onChange={(e) => setParam("type", e.target.value === "all" ? "" : e.target.value)}
              className="h-9 rounded-md border border-input bg-background px-2 text-sm"
            >
              <option value="all">All</option>
              <option value="Normal">Normal</option>
              <option value="Warning">Warning</option>
            </select>
          </label>
          <NamespaceSelector
            value={ns}
            onChange={(value) => setParam("ns", value)}
            namespaces={namespaces.data ?? []}
            isLoading={namespaces.isPending}
          />
        </div>
      </CardHeader>
      <CardContent>
        {feed.isPending ? (
          <div className="space-y-2" data-testid="events-loading">
            {[0, 1, 2, 3, 4].map((i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </div>
        ) : feed.isError ? (
          <ErrorState error={feed.error} onRetry={() => feed.refetch()} title="Failed to load events" />
        ) : rows.length === 0 ? (
          <EmptyState
            message={`No ${typeFilter === "all" ? "" : `${typeFilter.toLowerCase()} `}events${ns ? ` in namespace ${ns}` : ""}.`}
          />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Type</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead>Object</TableHead>
                  <TableHead>Message</TableHead>
                  <TableHead className="text-right">Count</TableHead>
                  <TableHead className="text-right">Last seen</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((event) => (
                  <EventRow key={`${event.namespace ?? ""}/${event.name}`} event={event} />
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function EventRow({ event }: { event: EventFeedRow }) {
  const io = event.involvedObject;
  const route = io.namespace && io.name ? routeForKind(io.kind, io.namespace, io.name) : undefined;

  return (
    <TableRow>
      <TableCell>
        <StatusBadge tone={eventTypeTone(event.type)}>{event.type}</StatusBadge>
      </TableCell>
      <TableCell className="font-medium">{event.reason}</TableCell>
      <TableCell className="whitespace-nowrap">
        {route ? (
          <Link to={route} className="underline-offset-4 hover:underline">
            {io.kind}/{io.name}
          </Link>
        ) : (
          <span className="text-muted-foreground">
            {io.kind}/{io.name}
          </span>
        )}
      </TableCell>
      <TableCell className="max-w-md break-words text-muted-foreground">{event.message}</TableCell>
      <TableCell className="text-right tabular-nums">{event.count > 1 ? `×${event.count}` : "—"}</TableCell>
      <TableCell className="text-right text-muted-foreground" title={event.lastSeen}>
        {formatAge(event.lastSeen)}
      </TableCell>
    </TableRow>
  );
}

