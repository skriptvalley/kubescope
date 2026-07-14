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
};

/** Builds the `?namespace=` query, or "" when no namespace is given. */
function nsQuery(namespace?: string): string {
  return namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
}
