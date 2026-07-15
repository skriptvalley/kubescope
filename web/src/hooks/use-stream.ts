import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import {
  api,
  streamResourceUrl,
  type EventFeedRow,
  type KubeObject,
  type ResourceList,
  type ResourceRef,
  type ResourceRow,
  type StreamFilter,
  type StreamGVR,
  type WorkloadSummary,
} from "@/lib/api";
import { queryKeys } from "@/lib/query-keys";
import { openStream, type StreamStatus, type WatchEvent } from "@/lib/stream";

export type { StreamStatus } from "@/lib/stream";

/** Poll cadence used only while the SSE stream is not live (the ADR-0006
 *  degradation path). A live stream sets refetchInterval to false. */
const POLL_INTERVAL_MS = 10_000;

function pollFor(status: StreamStatus): number | false {
  return status === "live" ? false : POLL_INTERVAL_MS;
}

/** Subscribes to a resource watch stream, routing events and resyncs to the
 *  caller via always-current refs (so the SSE connection is not torn down on
 *  every render). Returns the live connection status. When disabled, no stream
 *  is opened. */
function useWatchStream(
  gvr: StreamGVR,
  filter: StreamFilter,
  handlers: { onEvent: (event: WatchEvent) => void; onResync: () => void },
  enabled = true,
): StreamStatus {
  const [status, setStatus] = useState<StreamStatus>("connecting");
  const onEventRef = useRef(handlers.onEvent);
  const onResyncRef = useRef(handlers.onResync);
  onEventRef.current = handlers.onEvent;
  onResyncRef.current = handlers.onResync;

  const url = streamResourceUrl(gvr, filter);
  useEffect(() => {
    if (!enabled) return;
    return openStream(url, {
      onStatus: setStatus,
      onMessage: (data) => {
        let event: WatchEvent;
        try {
          event = JSON.parse(data) as WatchEvent;
        } catch {
          return;
        }
        if (event.type === "resync") onResyncRef.current();
        else onEventRef.current(event);
      },
      // A reconnect after a drop may have missed events; refetch a clean baseline.
      onReconnect: () => onResyncRef.current(),
    });
  }, [url, enabled]);

  return status;
}

// --- Cache-patch helpers (in-place, no full refetch) -------------------------

interface Identifiable {
  name: string;
  namespace?: string;
}

function rowKey(namespace: string | undefined, name: string): string {
  return `${namespace ?? ""}/${name}`;
}

function upsert<T extends Identifiable>(rows: T[], row: T): T[] {
  const key = rowKey(row.namespace, row.name);
  const index = rows.findIndex((r) => rowKey(r.namespace, r.name) === key);
  if (index === -1) return [...rows, row];
  const next = rows.slice();
  next[index] = row;
  return next;
}

function removeByRef<T extends Identifiable>(rows: T[], ref?: WatchEvent["ref"]): T[] {
  if (!ref) return rows;
  const key = rowKey(ref.namespace, ref.name);
  return rows.filter((r) => rowKey(r.namespace, r.name) !== key);
}

// --- Live list / detail / feed hooks -----------------------------------------

/** A generic resource list that live-patches rows from the SSE watch stream —
 *  add/update upsert a row, delete removes it, no full refetch. Falls back to
 *  polling while the stream is not live. */
export function useLiveResourceList(ref: ResourceRef) {
  const queryClient = useQueryClient();
  const key = queryKeys.resourceList(ref);
  const gvr: StreamGVR = { group: ref.group, version: ref.version, resource: ref.resource };

  const status = useWatchStream(
    gvr,
    { namespace: ref.namespace },
    {
      onEvent: (event) =>
        queryClient.setQueryData<ResourceList>(key, (prev) => {
          if (!prev) return prev;
          if (event.type === "delete") return { ...prev, rows: removeByRef(prev.rows, event.ref) };
          return { ...prev, rows: upsert(prev.rows, event.row as ResourceRow) };
        }),
      onResync: () => void queryClient.invalidateQueries({ queryKey: key }),
    },
  );

  const query = useQuery({
    queryKey: key,
    queryFn: () => api.resources.list(ref),
    refetchInterval: pollFor(status),
  });
  return { ...query, streamStatus: status };
}

/** A typed workload summary list that live-patches server-shaped rows from the
 *  watch stream (the row is the exact summary the list endpoint returns). */
export function useLiveWorkloadSummary<T extends WorkloadSummary>(
  resource: string,
  gvr: StreamGVR,
  namespace?: string,
) {
  const queryClient = useQueryClient();
  const key = queryKeys.workloadSummary(resource, namespace);

  const status = useWatchStream(
    gvr,
    { namespace },
    {
      onEvent: (event) =>
        queryClient.setQueryData<T[]>(key, (prev) => {
          if (!prev) return prev;
          if (event.type === "delete") return removeByRef(prev, event.ref);
          return upsert(prev, event.row as T);
        }),
      onResync: () => void queryClient.invalidateQueries({ queryKey: key }),
    },
  );

  const query = useQuery({
    queryKey: key,
    queryFn: () => api.workloads.list<T>(resource, namespace),
    refetchInterval: pollFor(status),
  });
  return { ...query, streamStatus: status };
}

/** A single object that live-updates from the watch stream (detail views).
 *  `deleted` flips true when the viewed object is removed cluster-side. The
 *  stream only runs while `enabled` (skipped for controller views that render
 *  their own data). */
export function useLiveResourceObject(ref: ResourceRef, enabled = true) {
  const queryClient = useQueryClient();
  const key = queryKeys.resourceObject(ref);
  const gvr: StreamGVR = { group: ref.group, version: ref.version, resource: ref.resource };
  const [deleted, setDeleted] = useState(false);

  const status = useWatchStream(
    gvr,
    { namespace: ref.namespace, name: ref.name, detail: true },
    {
      onEvent: (event) => {
        if (event.type === "delete") {
          setDeleted(true);
          return;
        }
        if (event.object) queryClient.setQueryData<KubeObject>(key, event.object as KubeObject);
      },
      onResync: () => void queryClient.invalidateQueries({ queryKey: key }),
    },
    enabled,
  );

  const query = useQuery({
    queryKey: key,
    queryFn: () => api.resources.get(ref),
    enabled,
  });
  return { ...query, streamStatus: status, deleted };
}

/** The live events feed (cluster-wide or per-namespace). Initial paint + poll
 *  fallback come from the feed endpoint; live rows arrive via the watch stream
 *  on core Events, kept newest-first. */
export function useEventsFeed(namespace?: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.eventsFeed(namespace);
  const gvr: StreamGVR = { group: "core", version: "v1", resource: "events" };

  const status = useWatchStream(
    gvr,
    { namespace },
    {
      onEvent: (event) =>
        queryClient.setQueryData<EventFeedRow[]>(key, (prev) => {
          if (!prev) return prev;
          if (event.type === "delete") return removeByRef(prev, event.ref);
          return sortByLastSeen(upsert(prev, event.row as EventFeedRow));
        }),
      onResync: () => void queryClient.invalidateQueries({ queryKey: key }),
    },
  );

  const query = useQuery({
    queryKey: key,
    queryFn: () => api.eventsFeed(namespace),
    refetchInterval: pollFor(status),
  });
  return { ...query, streamStatus: status };
}

/** Newest-first by last-seen. RFC3339 UTC timestamps sort lexicographically. */
function sortByLastSeen(rows: EventFeedRow[]): EventFeedRow[] {
  return [...rows].sort((a, b) => (b.lastSeen ?? "").localeCompare(a.lastSeen ?? ""));
}
