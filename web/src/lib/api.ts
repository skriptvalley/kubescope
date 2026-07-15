// Typed API client — the single place the frontend talks HTTP. Components
// never call fetch directly; they consume hooks that consume this module.

export interface NodeSummary {
  name: string;
  status: string;
  version: string;
}

interface NodeListResponse {
  items: NodeSummary[];
}

export interface ContextInfo {
  name: string;
  cluster: string;
  namespace: string;
  active: boolean;
}

export interface ContextHealth {
  name: string;
  reachable: boolean;
  authOK: boolean;
  serverVersion: string;
  error?: string;
  guidance?: string;
}

export interface Overview {
  context: string;
  serverVersion: string;
  nodeCount: number;
  namespaces: string[];
}

/** One resource type the active cluster serves (core kind or CRD). */
export interface APIResourceInfo {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespaced: boolean;
  verbs: string[];
}

/** One API group and its browsable resources. `name` is "" for the core group. */
export interface APIGroupInfo {
  name: string;
  resources: APIResourceInfo[];
}

export interface Discovery {
  groups: APIGroupInfo[];
  /** Per-group discovery failures; the reachable groups are still returned. */
  warnings?: string[];
}

/** A server-described list column. The frontend renders whatever the API says. */
export interface ResourceColumn {
  id: string;
  header: string;
}

export interface ResourceRow {
  name: string;
  namespace?: string;
  creationTimestamp?: string;
  uid?: string;
}

export interface ResourceList {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespaced: boolean;
  columns: ResourceColumn[];
  rows: ResourceRow[];
}

/** A generic Kubernetes object as returned by the get endpoint. */
export interface KubeObject {
  apiVersion?: string;
  kind?: string;
  metadata?: {
    name?: string;
    namespace?: string;
    uid?: string;
    creationTimestamp?: string;
    labels?: Record<string, string>;
    annotations?: Record<string, string>;
    ownerReferences?: OwnerReference[];
    [key: string]: unknown;
  };
  [key: string]: unknown;
}

export interface OwnerReference {
  apiVersion?: string;
  kind?: string;
  name?: string;
  uid?: string;
  controller?: boolean;
}

/** Path/query params identifying a resource collection or a single object. */
export interface ResourceRef {
  group: string;
  version: string;
  resource: string;
  namespace?: string;
  name?: string;
}

// --- Typed workload summaries (Sprint 3) -------------------------------------
// The seven core workload kinds get shaped rows with every field computed by the
// Go backend; the frontend only renders (ADR-0003). Every summary shares name,
// namespace and creationTimestamp, so shared table columns key off those.

/** A minimal controller owner reference for linking a resource to its owner. */
export interface WorkloadOwnerRef {
  kind: string;
  name: string;
  uid?: string;
}

export interface PodSummary {
  name: string;
  namespace: string;
  ready: string; // "readyContainers/totalContainers"
  readyContainers: number;
  totalContainers: number;
  status: string; // kubectl-style STATUS (Running, CrashLoopBackOff, Init:0/1, …)
  phase: string;
  restarts: number;
  node?: string;
  owner?: WorkloadOwnerRef;
  creationTimestamp?: string;
}

export interface DeploymentSummary {
  name: string;
  namespace: string;
  ready: string;
  desiredReplicas: number;
  readyReplicas: number;
  updatedReplicas: number;
  availableReplicas: number;
  rolloutStatus: string;
  creationTimestamp?: string;
}

export interface StatefulSetSummary {
  name: string;
  namespace: string;
  ready: string;
  desiredReplicas: number;
  readyReplicas: number;
  currentReplicas: number;
  updatedReplicas: number;
  rolloutStatus: string;
  creationTimestamp?: string;
}

export interface DaemonSetSummary {
  name: string;
  namespace: string;
  desired: number;
  current: number;
  ready: number;
  upToDate: number;
  available: number;
  rolloutStatus: string;
  creationTimestamp?: string;
}

export interface ReplicaSetSummary {
  name: string;
  namespace: string;
  ready: string;
  desiredReplicas: number;
  currentReplicas: number;
  readyReplicas: number;
  owner?: WorkloadOwnerRef;
  creationTimestamp?: string;
}

export interface JobSummary {
  name: string;
  namespace: string;
  completions: string;
  succeeded: number;
  failed: number;
  active: number;
  duration?: string;
  owner?: WorkloadOwnerRef;
  creationTimestamp?: string;
}

export interface CronJobSummary {
  name: string;
  namespace: string;
  schedule: string;
  suspend: boolean;
  active: number;
  lastScheduleTime?: string;
  creationTimestamp?: string;
}

/** Union of every typed workload row; all members share name/namespace/age. */
export type WorkloadSummary =
  | PodSummary
  | DeploymentSummary
  | StatefulSetSummary
  | DaemonSetSummary
  | ReplicaSetSummary
  | JobSummary
  | CronJobSummary;

/** One shaped event row (involvedObject-filtered, newest-first server-side). */
export interface EventSummary {
  type: string; // Normal | Warning
  reason: string;
  message: string;
  count: number;
  lastSeen?: string;
}

/** The object an event concerns; the feed deep-links to it when it exists. */
export interface InvolvedObjectRef {
  kind: string;
  namespace?: string;
  name: string;
}

/** One row of the cluster-wide/per-namespace events feed (Sprint 4). Name and
 *  namespace identify the Event object itself (rows key off them). uid and
 *  creationTimestamp make it a superset of the generic ResourceRow so the same
 *  streamed row renders correctly if events are browsed via the generic list. */
export interface EventFeedRow {
  name: string;
  namespace?: string;
  uid?: string;
  creationTimestamp?: string;
  type: string; // Normal | Warning
  reason: string;
  message: string;
  count: number;
  lastSeen?: string;
  involvedObject: InvolvedObjectRef;
}

interface WorkloadListResponse<T> {
  items: T[];
}

/** The URL token standing in for the empty core API group ("" is unusable in a path). */
export const CORE_GROUP_TOKEN = "core";

/** Maps a group name to its URL token, "" → "core". */
export function groupToken(group: string): string {
  return group === "" ? CORE_GROUP_TOKEN : group;
}

interface ContextListResponse {
  items: ContextInfo[];
}

interface HealthListResponse {
  items: ContextHealth[];
}

interface NamespaceListResponse {
  items: string[];
}

interface ObjectResponse {
  object: KubeObject;
}

interface YamlResponse {
  yaml: string;
}

interface ErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
    guidance?: string;
  };
}

/** Structured API failure carrying the backend's error envelope. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  /** Optional remediation text (e.g. ADR-0004 exec-plugin guidance). */
  readonly guidance?: string;

  constructor(message: string, code: string, status: number, guidance?: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.guidance = guidance;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { Accept: "application/json", ...init?.headers },
  });
  if (!response.ok) {
    throw await toApiError(response);
  }
  return (await response.json()) as T;
}

async function toApiError(response: Response): Promise<ApiError> {
  let code = "unknown_error";
  let message = `request failed with status ${response.status}`;
  let guidance: string | undefined;
  try {
    const body = (await response.json()) as ErrorEnvelope;
    code = body.error?.code ?? code;
    message = body.error?.message ?? message;
    guidance = body.error?.guidance;
  } catch {
    // Non-JSON error body; keep the generic message.
  }
  return new ApiError(message, code, response.status, guidance);
}

export const api = {
  nodes: {
    list: async (): Promise<NodeSummary[]> =>
      (await request<NodeListResponse>("/api/v1/nodes")).items,
  },
  contexts: {
    list: async (): Promise<ContextInfo[]> =>
      (await request<ContextListResponse>("/api/v1/contexts")).items,
    health: async (): Promise<ContextHealth[]> =>
      (await request<HealthListResponse>("/api/v1/contexts/health")).items,
    switch: async (name: string): Promise<ContextInfo[]> =>
      (
        await request<ContextListResponse>("/api/v1/contexts/switch", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name }),
        })
      ).items,
  },
  overview: async (): Promise<Overview> => request<Overview>("/api/v1/overview"),
  namespaces: {
    list: async (): Promise<string[]> =>
      (await request<NamespaceListResponse>("/api/v1/namespaces")).items,
  },
  resources: {
    discovery: async (refresh = false): Promise<Discovery> =>
      request<Discovery>(`/api/v1/discovery${refresh ? "?refresh=true" : ""}`),
    list: async (ref: ResourceRef): Promise<ResourceList> =>
      request<ResourceList>(`/api/v1/resources/${ref.group}/${ref.version}/${ref.resource}${nsQuery(ref.namespace)}`),
    get: async (ref: ResourceRef): Promise<KubeObject> =>
      (
        await request<ObjectResponse>(
          `/api/v1/resources/${ref.group}/${ref.version}/${ref.resource}/${encodeURIComponent(ref.name ?? "")}${nsQuery(ref.namespace)}`,
        )
      ).object,
    yaml: async (ref: ResourceRef): Promise<string> =>
      (
        await request<YamlResponse>(
          `/api/v1/resources/${ref.group}/${ref.version}/${ref.resource}/${encodeURIComponent(ref.name ?? "")}/yaml${nsQuery(ref.namespace)}`,
        )
      ).yaml,
  },
  workloads: {
    /** Typed summary list for one workload kind; T is the matching *Summary. */
    list: async <T>(resource: string, namespace?: string): Promise<T[]> =>
      (await request<WorkloadListResponse<T>>(`/api/v1/workloads/${resource}${nsQuery(namespace)}`)).items,
    /** The pods a controller owns (resolved server-side by selector + ownerRef). */
    ownedPods: async (resource: string, namespace: string, name: string): Promise<PodSummary[]> =>
      (
        await request<WorkloadListResponse<PodSummary>>(
          `/api/v1/workloads/${resource}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/pods`,
        )
      ).items,
    /** The Jobs a CronJob owns (its active + recent runs). */
    ownedJobs: async (namespace: string, name: string): Promise<JobSummary[]> =>
      (
        await request<WorkloadListResponse<JobSummary>>(
          `/api/v1/workloads/cronjobs/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/jobs`,
        )
      ).items,
  },
  /** Events filtered to a single object by involvedObject kind + name (+ namespace). */
  events: async (ref: { namespace?: string; kind: string; name: string }): Promise<EventSummary[]> => {
    const params = new URLSearchParams({ kind: ref.kind, name: ref.name });
    if (ref.namespace) params.set("namespace", ref.namespace);
    return (await request<WorkloadListResponse<EventSummary>>(`/api/v1/events?${params.toString()}`)).items;
  },
  /** Cluster-wide (or per-namespace) events feed — initial paint + polling
   *  fallback for the live events page. */
  eventsFeed: async (namespace?: string): Promise<EventFeedRow[]> =>
    (await request<WorkloadListResponse<EventFeedRow>>(`/api/v1/events/feed${nsQuery(namespace)}`)).items,
};

// --- Live streaming (SSE) URLs -----------------------------------------------
// The stream endpoints are consumed by EventSource, not fetch, so the API
// module only builds their URLs. `group` is the URL token already ("core" for
// the core group), matching the route params the pages hold.

/** Identifies a resource collection to watch. */
export interface StreamGVR {
  group: string; // URL token ("core" for the core group)
  version: string;
  resource: string;
}

/** Narrows a watch stream: a namespace and/or a single object, and whether to
 *  include the full object body (detail views need it; lists do not). */
export interface StreamFilter {
  namespace?: string;
  name?: string;
  detail?: boolean;
}

/** Builds the SSE URL for a resource watch stream. */
export function streamResourceUrl(gvr: StreamGVR, filter: StreamFilter = {}): string {
  const params = new URLSearchParams();
  if (filter.namespace) params.set("namespace", filter.namespace);
  if (filter.name) params.set("name", filter.name);
  if (filter.detail) params.set("detail", "true");
  const query = params.toString();
  return `/api/v1/stream/resources/${gvr.group}/${gvr.version}/${gvr.resource}${query ? `?${query}` : ""}`;
}

/** Pod-log stream parameters (Story 4.3). */
export interface PodLogParams {
  container?: string;
  follow?: boolean;
  previous?: boolean;
  tailLines?: number;
}

/** Builds the SSE URL for a pod log stream. */
export function podLogsUrl(namespace: string, name: string, params: PodLogParams = {}): string {
  const q = new URLSearchParams();
  if (params.container) q.set("container", params.container);
  if (params.follow === false) q.set("follow", "false");
  if (params.previous) q.set("previous", "true");
  if (params.tailLines !== undefined) q.set("tailLines", String(params.tailLines));
  const query = q.toString();
  return `/api/v1/stream/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs${query ? `?${query}` : ""}`;
}

/** Builds the `?namespace=` query, or "" when no namespace is given. */
function nsQuery(namespace?: string): string {
  return namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
}
