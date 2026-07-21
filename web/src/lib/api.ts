// Typed API client — the single place the frontend talks HTTP. Components
// never call fetch directly; they consume hooks that consume this module.

export interface NodeSummary {
  name: string;
  status: string;
  version: string;
  /** spec.unschedulable — backs the schedulability badge and cordon toggle. */
  unschedulable: boolean;
}

/** Server posture the UI reflects (read-only, auth mode). Read-only enforcement
 *  is server-side middleware; this only drives control visibility (ADR-0005). */
export interface ServerConfig {
  readOnly: boolean;
  authMode: string;
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
  /** Failure taxonomy class (FB-6) — e.g. connection_refused, tls_cert. */
  reason?: string;
  /** Deep-link to the relevant remediation doc, when one applies. */
  docURL?: string;
}

/** First-run / connectivity posture of the server (FB-6). Drives the starter
 *  page and the unreachable banner. Always returned 200 by GET /api/v1/setup. */
export interface SetupState {
  state:
    | "no_kubeconfig"
    | "no_contexts"
    | "no_active_context"
    | "active_unreachable"
    | "ready";
  /** Machine reason: kubeconfig_missing | kubeconfig_invalid | no_current_context
   *  | a FailureClass | "". */
  reason?: string;
  message?: string;
  guidance?: string;
  docURL?: string;
  /** Registered kubeconfig source paths in precedence order (not expanded files)
   *  — the source registry that feeds the merged kubeconfig (FB-8). */
  kubeconfigSources: string[];
  activeContext?: string;
  canSetKubeconfig: boolean;
}

// --- Kubeconfig source registry (FB-8) ---------------------------------------
// A registry of kubeconfig sources (files or directories) merged in precedence
// order. Each source expands to zero or more kubeconfig files; contexts win
// first-in-precedence and shadowed duplicates are surfaced.

/** One kubeconfig file inside a directory source (or the file source itself). */
export interface KubeconfigSourceFile {
  path: string;
  status: "ok" | "unparseable" | "too_large" | "hidden";
  /** Classified detail when not ok (e.g. the parse error); omitted when ok. */
  message?: string;
  /** Context names this file contributes (its winning definitions). */
  contexts?: string[];
  /** Context names defined here but won by an earlier file. */
  shadowed?: string[];
}

/** One registered kubeconfig source (a file or a directory of kubeconfigs). */
export interface KubeconfigSource {
  /** First 12 hex of sha256(path). Stable, URL-safe; used for DELETE. */
  id: string;
  path: string;
  kind: "file" | "dir";
  origin: "env" | "runtime";
  status: "ok" | "missing" | "unparseable" | "empty";
  /** Classified detail when not ok; omitted when ok. */
  message?: string;
  /** Directory sources only, lexicographic; omitted for file sources. */
  files?: KubeconfigSourceFile[];
  /** Context names whose winning definition comes from this source. */
  contexts?: string[];
  /** Context names defined here but won by an earlier source. */
  shadowed?: string[];
}

export interface KubeconfigSourceList {
  sources: KubeconfigSource[];
  /** Whether the runtime add/remove controls are permitted (server folds in the
   *  KUBESCOPE_ALLOW_KUBECONFIG_SET flag and read-only mode). */
  canSetKubeconfig: boolean;
}

export interface Overview {
  context: string;
  serverVersion: string;
  nodeCount: number;
  namespaces: string[];
}

// --- Data enhancements (ADR-0009, Story F) -----------------------------------

/** Summed CPU/Memory usage for one pod (metrics-server). */
export interface PodMetrics {
  name: string;
  namespace: string;
  cpu: string; // millicores, e.g. "71m"
  memory: string; // binary, e.g. "306Mi"
}

/** Pod metrics plus whether metrics-server was reachable (false ⇒ render "—"). */
export interface PodMetricsResponse {
  available: boolean;
  items: PodMetrics[];
}

/** Per-resource-type counts for the sidebar, keyed "group/version/resource"
 *  (raw group, "" for core — the same key discovery-nav builds). */
export interface CountsResponse {
  counts: Record<string, number>;
  /** At least one type could not be counted (render it without a count). */
  partial: boolean;
}

/** One used/hard pair within a namespace ResourceQuota. `percent` (0–100) is
 *  computed server-side so the bar never parses mixed units. */
export interface QuotaEntry {
  quotaName: string;
  resource: string;
  used: string;
  hard: string;
  percent: number;
}

export interface QuotasResponse {
  items: QuotaEntry[];
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
  /** Values for any per-kind enrichment columns, keyed by column id (Sprint 7).
   *  The table reads a non-name/namespace/age column's value from here. */
  cells?: Record<string, string>;
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

// --- Typed Service detail (Sprint 7) -----------------------------------------

export interface ServicePortSummary {
  name?: string;
  port: number;
  protocol: string;
  targetPort?: string;
  nodePort?: number;
}

/** Points a resolved endpoint address at the pod behind it (deep-link target). */
export interface EndpointTargetRef {
  kind: string;
  namespace?: string;
  name: string;
}

export interface EndpointAddressSummary {
  ip: string;
  hostname?: string;
  nodeName?: string;
  ready: boolean;
  targetRef?: EndpointTargetRef;
}

/** A Service summary plus its resolved Endpoints (ready + not-ready backing
 *  pods). The address lists are the Service's matching pod list, split by
 *  readiness; each address links to its pod via targetRef. */
export interface ServiceDetail {
  name: string;
  namespace: string;
  type: string;
  clusterIP?: string;
  clusterIPs?: string[];
  externalIPs?: string[];
  sessionAffinity?: string;
  selector?: Record<string, string>;
  ports: ServicePortSummary[];
  endpointsFound: boolean;
  readyAddresses: EndpointAddressSummary[];
  notReadyAddresses: EndpointAddressSummary[];
}

// --- Global search (Sprint 7) ------------------------------------------------

/** One name match. `group` is the raw API group ("" for core); tokenize it with
 *  groupToken() to build the resource route. */
export interface SearchResult {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespace?: string;
  name: string;
  namespaced: boolean;
}

export interface SearchResponse {
  query: string;
  results: SearchResult[];
  /** More matches existed than the limit returned. */
  truncated: boolean;
  /** Types that failed to list — results are partial (degraded, not failed). */
  warnings?: string[];
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
    docURL?: string;
  };
}

/** Structured API failure carrying the backend's error envelope. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  /** Optional remediation text (e.g. ADR-0004 exec-plugin guidance). */
  readonly guidance?: string;
  /** Optional deep-link to the relevant remediation doc (FB-6). */
  readonly docURL?: string;

  constructor(message: string, code: string, status: number, guidance?: string, docURL?: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.guidance = guidance;
    this.docURL = docURL;
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
  let docURL: string | undefined;
  try {
    const body = (await response.json()) as ErrorEnvelope;
    code = body.error?.code ?? code;
    message = body.error?.message ?? message;
    guidance = body.error?.guidance;
    docURL = body.error?.docURL;
  } catch {
    // Non-JSON error body; keep the generic message.
  }
  return new ApiError(message, code, response.status, guidance, docURL);
}

/** Per-pod outcome of a node drain. */
export interface PodDrainResult {
  namespace: string;
  name: string;
  result: "evicted" | "skipped" | "blocked" | "error";
  reason?: string;
}

/** Whole-node drain result: every pod plus tallies. */
export interface DrainResult {
  node: string;
  pods: PodDrainResult[];
  evicted: number;
  skipped: number;
  blocked: number;
  failed: number;
}

/** One active backend-managed port-forward (Sprint 6). */
export interface PortForward {
  id: string;
  context: string;
  namespace: string;
  pod: string;
  localPort: number;
  remotePort: number;
  startedAt: string;
}

interface PortForwardListResponse {
  items: PortForward[];
}

/** Parameters for starting a port-forward: 0 localPort auto-assigns. */
export interface StartPortForwardParams {
  namespace: string;
  pod: string;
  remotePort: number;
  localPort?: number;
}

export const api = {
  config: async (): Promise<ServerConfig> => request<ServerConfig>("/api/v1/config"),
  /** First-run / connectivity posture (FB-6). Unguarded; always 200. */
  setup: {
    state: async (): Promise<SetupState> => request<SetupState>("/api/v1/setup"),
  },
  /** Kubeconfig source registry (FB-8). Listing is unguarded (always 200);
   *  add/remove are guarded by KUBESCOPE_ALLOW_KUBECONFIG_SET and read-only mode
   *  server-side, reflected in the listing's canSetKubeconfig. Every mutation
   *  returns the fresh listing. */
  kubeconfigs: {
    list: async (): Promise<KubeconfigSourceList> =>
      request<KubeconfigSourceList>("/api/v1/kubeconfigs"),
    add: async (path: string): Promise<KubeconfigSourceList> =>
      request<KubeconfigSourceList>("/api/v1/kubeconfigs", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path }),
      }),
    remove: async (id: string): Promise<KubeconfigSourceList> =>
      request<KubeconfigSourceList>(`/api/v1/kubeconfigs/${encodeURIComponent(id)}`, {
        method: "DELETE",
      }),
  },
  nodes: {
    list: async (): Promise<NodeSummary[]> =>
      (await request<NodeListResponse>("/api/v1/nodes")).items,
    /** Cordon (unschedulable=true) or uncordon (false) a node. */
    setSchedulable: async (name: string, cordon: boolean): Promise<void> => {
      await request(`/api/v1/nodes/${encodeURIComponent(name)}/${cordon ? "cordon" : "uncordon"}`, {
        method: "POST",
      });
    },
    /** Drain a node: cordon, then evict eligible pods via the eviction API. */
    drain: async (name: string): Promise<DrainResult> =>
      request<DrainResult>(`/api/v1/nodes/${encodeURIComponent(name)}/drain`, { method: "POST" }),
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
    /** ResourceQuota bars for a namespace (ADR-0009); empty when none exist. */
    quotas: async (namespace: string): Promise<QuotaEntry[]> =>
      (
        await request<QuotasResponse>(
          `/api/v1/namespaces/${encodeURIComponent(namespace)}/quotas`,
        )
      ).items,
  },
  /** Per-type resource counts for the sidebar (ADR-0009); best-effort/partial. */
  counts: async (): Promise<CountsResponse> => request<CountsResponse>("/api/v1/counts"),
  /** Pod CPU/Memory from metrics-server (ADR-0009). `available:false` ⇒ render "—". */
  metrics: {
    pods: async (namespace?: string): Promise<PodMetricsResponse> =>
      request<PodMetricsResponse>(`/api/v1/metrics/pods${nsQuery(namespace)}`),
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
    /** Apply an edited manifest via the dynamic client (any GVR incl. CRDs). A
     *  stale resourceVersion surfaces as an ApiError with code "conflict". */
    apply: async (ref: ResourceRef, yaml: string): Promise<KubeObject> =>
      (
        await request<ObjectResponse>(
          `/api/v1/resources/${ref.group}/${ref.version}/${ref.resource}/${encodeURIComponent(ref.name ?? "")}${nsQuery(ref.namespace)}`,
          {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ yaml }),
          },
        )
      ).object,
    /** Delete any object (namespaced or cluster-scoped) via the dynamic client. */
    delete: async (ref: ResourceRef): Promise<void> => {
      await request(
        `/api/v1/resources/${ref.group}/${ref.version}/${ref.resource}/${encodeURIComponent(ref.name ?? "")}${nsQuery(ref.namespace)}`,
        { method: "DELETE" },
      );
    },
  },
  secrets: {
    /** The plaintext of a single Secret key, fetched on explicit reveal. */
    reveal: async (namespace: string, name: string, key: string): Promise<string> =>
      (
        await request<{ key: string; value: string }>(
          `/api/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/reveal?key=${encodeURIComponent(key)}`,
        )
      ).value,
  },
  services: {
    /** A Service's summary + resolved Endpoints (ready/not-ready backing pods). */
    detail: async (namespace: string, name: string): Promise<ServiceDetail> =>
      request<ServiceDetail>(
        `/api/v1/services/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      ),
  },
  /** Name search across the active context's discovered types (bounded, partial-
   *  tolerant). Navigates to the matched object's detail route on select. */
  search: async (query: string, limit?: number): Promise<SearchResponse> => {
    const params = new URLSearchParams({ q: query });
    if (limit !== undefined) params.set("limit", String(limit));
    return request<SearchResponse>(`/api/v1/search?${params.toString()}`);
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
    /** Set the replica count (Deployments/StatefulSets/ReplicaSets) via the
     *  scale subresource. */
    scale: async (resource: string, namespace: string, name: string, replicas: number): Promise<void> => {
      await request(
        `/api/v1/workloads/${resource}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/scale`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ replicas }),
        },
      );
    },
    /** Trigger a rollout-restart (Deployments/StatefulSets/DaemonSets). */
    restart: async (resource: string, namespace: string, name: string): Promise<void> => {
      await request(
        `/api/v1/workloads/${resource}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/restart`,
        { method: "POST" },
      );
    },
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
  /** Backend-managed pod port-forwards (Sprint 6): start, list, stop. */
  portForwards: {
    list: async (): Promise<PortForward[]> =>
      (await request<PortForwardListResponse>("/api/v1/portforwards")).items,
    /** Start a forward (pod port → backend loopback listener). Starting is a
     *  mutating control, so it is rejected in read-only mode server-side. */
    start: async (params: StartPortForwardParams): Promise<PortForward> =>
      request<PortForward>("/api/v1/portforwards", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(params),
      }),
    /** Stop a forward by id (idempotent from the UI's perspective). */
    stop: async (id: string): Promise<void> => {
      await request(`/api/v1/portforwards/${encodeURIComponent(id)}`, { method: "DELETE" });
    },
  },
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

// --- Exec terminal (WebSocket) URL --------------------------------------------
// The exec endpoint is a WebSocket, not SSE/fetch, so the API module only builds
// its (relative) URL; lib/exec-socket upgrades it to a ws(s):// connection.

/** Exec target parameters (Story 6.1): container and command (default shell). */
export interface PodExecParams {
  container?: string;
  command?: string[];
}

/** Builds the exec WebSocket URL for a pod/container (relative path). */
export function podExecUrl(namespace: string, name: string, params: PodExecParams = {}): string {
  const q = new URLSearchParams();
  if (params.container) q.set("container", params.container);
  for (const c of params.command ?? []) q.append("command", c);
  const query = q.toString();
  return `/api/v1/stream/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/exec${query ? `?${query}` : ""}`;
}

/** Builds the `?namespace=` query, or "" when no namespace is given. */
function nsQuery(namespace?: string): string {
  return namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
}
