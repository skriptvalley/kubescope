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

interface ContextListResponse {
  items: ContextInfo[];
}

interface HealthListResponse {
  items: ContextHealth[];
}

interface ErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
  };
}

/** Structured API failure carrying the backend's error envelope. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(message: string, code: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
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
  try {
    const body = (await response.json()) as ErrorEnvelope;
    code = body.error?.code ?? code;
    message = body.error?.message ?? message;
  } catch {
    // Non-JSON error body; keep the generic message.
  }
  return new ApiError(message, code, response.status);
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
};
