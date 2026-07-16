import { useQuery, useQueryClient, type QueryClient, type QueryKey } from "@tanstack/react-query";
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
import { connectivity } from "@/lib/connectivity";
import { queryKeys } from "@/lib/query-keys";
import { openStream, type StatusInfo, type StreamStatus, type WatchEvent } from "@/lib/stream";

export type { StreamStatus } from "@/lib/stream";

/** Base/max poll cadence used only while the SSE stream is not live (the ADR-0006
 *  degradation path). A live stream sets refetchInterval to false. While not
 *  live the interval grows exponentially with consecutive fetch failures (FB-6
 *  Story D) so a down cluster is not hammered at full cadence. */
const POLL_BASE_MS = 10_000;
const POLL_MAX_MS = 60_000;

/** Failure reasons scoped to one resource/credential rather than the whole
 *  cluster: they must not raise the app-wide unreachable banner. */
const SCOPED_REASONS = new Set(["forbidden", "auth_expired"]);

/** Exported for unit tests. `fetchFailureCount` comes from the query's own state
 *  (`query.state.fetchFailureCount`); it resets to 0 on any successful fetch. */
export function pollFor(status: StreamStatus, fetchFailureCount = 0): number | false {
  if (status === "live") return false;
  return Math.min(POLL_BASE_MS * 2 ** fetchFailureCount, POLL_MAX_MS);
}

/** Subscribes to a resource watch stream, routing events and resyncs to the
 *  caller via always-current refs (so the SSE connection is not torn down on
 *  every render). Returns the live connection status plus the current
 *  connectivity `StatusInfo` (non-null while the backend reports the cluster
 *  unreachable, FB-6 Story D). When disabled, no stream is opened. */
function useWatchStream(
  gvr: StreamGVR,
  filter: StreamFilter,
  handlers: { onEvent: (event: WatchEvent) => void; onResync: () => void },
  enabled = true,
): { status: StreamStatus; unreachable: StatusInfo | null } {
  const [status, setStatus] = useState<StreamStatus>("connecting");
  const [unreachable, setUnreachable] = useState<StatusInfo | null>(null);
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
        if (event.type === "status") {
          const info = event.status;
          if (!info) return;
          if (info.state === "unreachable") {
            setUnreachable(info);
            // Only cluster-level failures flip the app-wide banner. A 403/401
            // on ONE resource's watch is scoped to that resource (the FB-6
            // matrix: "insufficient permissions for this resource, not a
            // global failure") — the view's own error surface covers it.
            if (!SCOPED_REASONS.has(info.reason ?? "")) {
              connectivity.setActiveUnreachable(true);
            }
          } else {
            setUnreachable(null);
            connectivity.setActiveUnreachable(false);
            // Recovery: the backend also sends a resync, but invalidate here too
            // so a clean baseline is refetched even if the resync is missed.
            onResyncRef.current();
          }
          return;
        }
        if (event.type === "resync") onResyncRef.current();
        else onEventRef.current(event);
      },
      // A reconnect after a drop may have missed events; refetch a clean baseline.
      onReconnect: () => onResyncRef.current(),
    });
  }, [url, enabled]);

  return { status, unreachable };
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

/** Applies a watch event to a cached collection in place. When the baseline is
 *  not yet populated (an event raced the initial fetch), the event cannot be
 *  patched, so it flags `missed`; useFlushMissedEvents then invalidates once the
 *  baseline lands — capturing it instead of silently dropping it until resync.
 *  Invalidating here would be a no-op: it dedupes into the in-flight fetch,
 *  whose older snapshot need not contain the raced object. */
function patchCollection<C>(
  queryClient: QueryClient,
  key: QueryKey,
  missed: { current: boolean },
  apply: (prev: C) => C,
): void {
  if (queryClient.getQueryData<C>(key) === undefined) {
    missed.current = true;
    return;
  }
  queryClient.setQueryData<C>(key, (prev) => (prev === undefined ? prev : apply(prev)));
}

/** Once the baseline query has loaded, refetch it if any event was dropped while
 *  it was in flight — a fresh fetch (not deduped into the initial one) captures
 *  the raced object. Fires at most once per missed burst. */
function useFlushMissedEvents(
  queryClient: QueryClient,
  key: QueryKey,
  ready: boolean,
  missed: { current: boolean },
): void {
  const keyStr = JSON.stringify(key);
  useEffect(() => {
    if (ready && missed.current) {
      missed.current = false;
      void queryClient.invalidateQueries({ queryKey: key });
    }
    // key is captured via its stable string form; queryClient/missed are stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, keyStr]);
}

// --- Live list / detail / feed hooks -----------------------------------------

/** A generic resource list that live-patches rows from the SSE watch stream —
 *  add/update upsert a row, delete removes it, no full refetch. Falls back to
 *  polling while the stream is not live. */
export function useLiveResourceList(ref: ResourceRef) {
  const queryClient = useQueryClient();
  const key = queryKeys.resourceList(ref);
  const gvr: StreamGVR = { group: ref.group, version: ref.version, resource: ref.resource };
  const missed = useRef(false);

  const { status, unreachable } = useWatchStream(
    gvr,
    { namespace: ref.namespace },
    {
      onEvent: (event) =>
        patchCollection<ResourceList>(queryClient, key, missed, (prev) =>
          event.type === "delete"
            ? { ...prev, rows: removeByRef(prev.rows, event.ref) }
            : { ...prev, rows: upsert(prev.rows, event.row as ResourceRow) },
        ),
      onResync: () => void queryClient.invalidateQueries({ queryKey: key }),
    },
  );

  const query = useQuery({
    queryKey: key,
    queryFn: () => api.resources.list(ref),
    refetchInterval: (q) => pollFor(status, q.state.fetchFailureCount),
  });
  useFlushMissedEvents(queryClient, key, query.isSuccess, missed);
  return { ...query, streamStatus: status, unreachable };
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
  const missed = useRef(false);

  const { status, unreachable } = useWatchStream(
    gvr,
    { namespace },
    {
      onEvent: (event) =>
        patchCollection<T[]>(queryClient, key, missed, (prev) =>
          event.type === "delete" ? removeByRef(prev, event.ref) : upsert(prev, event.row as T),
        ),
      onResync: () => void queryClient.invalidateQueries({ queryKey: key }),
    },
  );

  const query = useQuery({
    queryKey: key,
    queryFn: () => api.workloads.list<T>(resource, namespace),
    refetchInterval: (q) => pollFor(status, q.state.fetchFailureCount),
  });
  useFlushMissedEvents(queryClient, key, query.isSuccess, missed);
  return { ...query, streamStatus: status, unreachable };
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

  // The detail route reuses this component across objects (only params change),
  // so clear a prior object's deleted flag when the viewed object changes.
  useEffect(() => {
    setDeleted(false);
  }, [ref.namespace, ref.name]);

  const { status, unreachable } = useWatchStream(
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
    // Poll fallback while the stream is not live (AC 4.2), but not after the
    // object is gone — there is nothing to poll for.
    refetchInterval: (q) => (deleted ? false : pollFor(status, q.state.fetchFailureCount)),
  });
  return { ...query, streamStatus: status, deleted, unreachable };
}

/** The live events feed (cluster-wide or per-namespace). Initial paint + poll
 *  fallback come from the feed endpoint; live rows arrive via the watch stream
 *  on core Events, kept newest-first. */
export function useEventsFeed(namespace?: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.eventsFeed(namespace);
  const gvr: StreamGVR = { group: "core", version: "v1", resource: "events" };
  const missed = useRef(false);

  const { status, unreachable } = useWatchStream(
    gvr,
    { namespace },
    {
      onEvent: (event) =>
        patchCollection<EventFeedRow[]>(queryClient, key, missed, (prev) =>
          event.type === "delete"
            ? removeByRef(prev, event.ref)
            : sortByLastSeen(upsert(prev, event.row as EventFeedRow)),
        ),
      onResync: () => void queryClient.invalidateQueries({ queryKey: key }),
    },
  );

  const query = useQuery({
    queryKey: key,
    queryFn: () => api.eventsFeed(namespace),
    refetchInterval: (q) => pollFor(status, q.state.fetchFailureCount),
  });
  useFlushMissedEvents(queryClient, key, query.isSuccess, missed);
  return { ...query, streamStatus: status, unreachable };
}

/** Newest-first by last-seen. RFC3339 UTC timestamps sort lexicographically. */
function sortByLastSeen(rows: EventFeedRow[]): EventFeedRow[] {
  return [...rows].sort((a, b) => (b.lastSeen ?? "").localeCompare(a.lastSeen ?? ""));
}
