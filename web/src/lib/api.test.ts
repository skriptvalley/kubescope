import { afterEach, describe, expect, it, vi } from "vitest";

import { api, ApiError } from "./api";

function stubFetch(response: Partial<Response> & { json?: () => Promise<unknown> }) {
  const stub = vi.fn().mockResolvedValue(response as Response);
  vi.stubGlobal("fetch", stub);
  return stub;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("api.nodes.list", () => {
  it("returns items from the list envelope", async () => {
    const fetchStub = stubFetch({
      ok: true,
      status: 200,
      json: async () => ({
        items: [{ name: "node-a", status: "Ready", version: "v1.31.0" }],
      }),
    });

    const nodes = await api.nodes.list();

    expect(nodes).toEqual([{ name: "node-a", status: "Ready", version: "v1.31.0" }]);
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/v1/nodes",
      expect.objectContaining({ headers: expect.objectContaining({ Accept: "application/json" }) }),
    );
  });

  it("throws ApiError with the backend error envelope", async () => {
    stubFetch({
      ok: false,
      status: 503,
      json: async () => ({
        error: { code: "kubeconfig_unavailable", message: "cannot load kubeconfig" },
      }),
    });

    const err = await api.nodes.list().catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    const apiErr = err as ApiError;
    expect(apiErr.code).toBe("kubeconfig_unavailable");
    expect(apiErr.status).toBe(503);
    expect(apiErr.message).toBe("cannot load kubeconfig");
  });

  it("carries the guidance field from the error envelope", async () => {
    stubFetch({
      ok: false,
      status: 502,
      json: async () => ({
        error: {
          code: "cluster_unreachable",
          message: "exec: aws not found",
          guidance: "mount ~/.aws — see ADR-0004",
        },
      }),
    });

    const err = (await api.overview().catch((e: unknown) => e)) as ApiError;
    expect(err.code).toBe("cluster_unreachable");
    expect(err.guidance).toBe("mount ~/.aws — see ADR-0004");
  });

  it("carries the docURL from the error envelope", async () => {
    stubFetch({
      ok: false,
      status: 502,
      json: async () => ({
        error: {
          code: "tls_cert",
          message: "x509: certificate signed by unknown authority",
          guidance: "mount the cluster CA",
          docURL: "https://example/adr-0004",
        },
      }),
    });

    const err = (await api.overview().catch((e: unknown) => e)) as ApiError;
    expect(err.code).toBe("tls_cert");
    expect(err.docURL).toBe("https://example/adr-0004");
  });

  it("throws a generic ApiError when the error body is not JSON", async () => {
    stubFetch({
      ok: false,
      status: 502,
      json: async () => {
        throw new SyntaxError("not json");
      },
    });

    const err = await api.nodes.list().catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    const apiErr = err as ApiError;
    expect(apiErr.code).toBe("unknown_error");
    expect(apiErr.status).toBe(502);
    expect(apiErr.message).toContain("502");
  });
});

describe("api.contexts", () => {
  it("lists contexts from the envelope", async () => {
    stubFetch({
      ok: true,
      status: 200,
      json: async () => ({
        items: [{ name: "prod", cluster: "c", namespace: "default", active: true }],
      }),
    });

    const contexts = await api.contexts.list();
    expect(contexts).toHaveLength(1);
    expect(contexts[0].active).toBe(true);
  });

  it("posts the target name when switching and returns the new list", async () => {
    const fetchStub = stubFetch({
      ok: true,
      status: 200,
      json: async () => ({ items: [{ name: "dev", cluster: "c", namespace: "default", active: true }] }),
    });

    const contexts = await api.contexts.switch("dev");

    expect(contexts[0].name).toBe("dev");
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/v1/contexts/switch",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ name: "dev" }),
      }),
    );
  });
});

describe("api.setup", () => {
  it("fetches the setup state with its kubeconfig sources", async () => {
    const fetchStub = stubFetch({
      ok: true,
      status: 200,
      json: async () => ({
        state: "ready",
        kubeconfigSources: ["/kubeconfig", "/extra"],
        activeContext: "prod",
        canSetKubeconfig: true,
      }),
    });

    const state = await api.setup.state();
    expect(state.state).toBe("ready");
    expect(state.activeContext).toBe("prod");
    expect(state.kubeconfigSources).toEqual(["/kubeconfig", "/extra"]);
    expect(fetchStub).toHaveBeenCalledWith("/api/v1/setup", expect.anything());
  });
});

describe("api.kubeconfigs", () => {
  const listing = {
    sources: [
      {
        id: "abc123",
        path: "/kubeconfig",
        kind: "dir",
        origin: "env",
        status: "ok",
        files: [{ path: "/kubeconfig/a.yaml", status: "ok", contexts: ["kind-a"] }],
        contexts: ["kind-a"],
      },
    ],
    canSetKubeconfig: true,
  };

  it("GETs the source listing", async () => {
    const fetchStub = stubFetch({ ok: true, status: 200, json: async () => listing });

    const result = await api.kubeconfigs.list();
    expect(result.canSetKubeconfig).toBe(true);
    expect(result.sources[0].id).toBe("abc123");
    expect(fetchStub).toHaveBeenCalledWith("/api/v1/kubeconfigs", expect.anything());
  });

  it("POSTs the path when adding a source and returns the fresh listing", async () => {
    const fetchStub = stubFetch({ ok: true, status: 200, json: async () => listing });

    const result = await api.kubeconfigs.add("/kubeconfig");
    expect(result.sources).toHaveLength(1);
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/v1/kubeconfigs",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ path: "/kubeconfig" }) }),
    );
  });

  it("DELETEs a source by id and returns the fresh listing", async () => {
    const fetchStub = stubFetch({
      ok: true,
      status: 200,
      json: async () => ({ sources: [], canSetKubeconfig: true }),
    });

    const result = await api.kubeconfigs.remove("abc123");
    expect(result.sources).toHaveLength(0);
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/v1/kubeconfigs/abc123",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("surfaces the backend guidance envelope on a rejected add", async () => {
    stubFetch({
      ok: false,
      status: 422,
      json: async () => ({
        error: {
          code: "kubeconfig_invalid",
          message: "path not visible",
          guidance: "mount a directory once (-v ~/.kube:/kubeconfigs:ro)",
        },
      }),
    });

    const err = (await api.kubeconfigs.add("/missing").catch((e: unknown) => e)) as ApiError;
    expect(err).toBeInstanceOf(ApiError);
    expect(err.code).toBe("kubeconfig_invalid");
    expect(err.status).toBe(422);
    expect(err.guidance).toContain("mount a directory");
  });
});

describe("api.overview", () => {
  it("returns the overview object", async () => {
    stubFetch({
      ok: true,
      status: 200,
      json: async () => ({
        context: "prod",
        serverVersion: "v1.33.0",
        nodeCount: 2,
        namespaces: ["default"],
      }),
    });

    const overview = await api.overview();
    expect(overview.serverVersion).toBe("v1.33.0");
    expect(overview.nodeCount).toBe(2);
  });
});

describe("api.namespaces.list", () => {
  it("returns names from the envelope", async () => {
    stubFetch({ ok: true, status: 200, json: async () => ({ items: ["default", "kube-system"] }) });
    expect(await api.namespaces.list()).toEqual(["default", "kube-system"]);
  });
});

describe("api.resources", () => {
  it("requests discovery, with refresh when asked", async () => {
    const fetchStub = stubFetch({ ok: true, status: 200, json: async () => ({ groups: [] }) });
    await api.resources.discovery();
    expect(fetchStub).toHaveBeenCalledWith("/api/v1/discovery", expect.anything());

    await api.resources.discovery(true);
    expect(fetchStub).toHaveBeenCalledWith("/api/v1/discovery?refresh=true", expect.anything());
  });

  it("lists a namespaced resource with the namespace query", async () => {
    const fetchStub = stubFetch({
      ok: true,
      status: 200,
      json: async () => ({ columns: [], rows: [], namespaced: true }),
    });
    await api.resources.list({ group: "apps", version: "v1", resource: "deployments", namespace: "default" });
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/v1/resources/apps/v1/deployments?namespace=default",
      expect.anything(),
    );
  });

  it("lists across all namespaces when no namespace is given", async () => {
    const fetchStub = stubFetch({
      ok: true,
      status: 200,
      json: async () => ({ columns: [], rows: [], namespaced: true }),
    });
    await api.resources.list({ group: "core", version: "v1", resource: "pods" });
    expect(fetchStub).toHaveBeenCalledWith("/api/v1/resources/core/v1/pods", expect.anything());
  });

  it("unwraps a single object from the envelope", async () => {
    stubFetch({
      ok: true,
      status: 200,
      json: async () => ({ object: { kind: "Pod", metadata: { name: "nginx" } } }),
    });
    const obj = await api.resources.get({
      group: "core",
      version: "v1",
      resource: "pods",
      namespace: "default",
      name: "nginx",
    });
    expect(obj.kind).toBe("Pod");
    expect(obj.metadata?.name).toBe("nginx");
  });

  it("unwraps the yaml string from the envelope", async () => {
    const fetchStub = stubFetch({ ok: true, status: 200, json: async () => ({ yaml: "kind: Pod\n" }) });
    const yaml = await api.resources.yaml({
      group: "core",
      version: "v1",
      resource: "pods",
      namespace: "default",
      name: "nginx",
    });
    expect(yaml).toBe("kind: Pod\n");
    expect(fetchStub).toHaveBeenCalledWith(
      "/api/v1/resources/core/v1/pods/nginx/yaml?namespace=default",
      expect.anything(),
    );
  });
});
