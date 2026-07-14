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
