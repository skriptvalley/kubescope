import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { KubeObject, ResourceList } from "@/lib/api";
import { FakeEventSource, installFakeEventSource } from "@/test/fake-event-source";

import { useLiveResourceList, useLiveResourceObject, useLiveWorkloadSummary } from "./use-stream";

const listMock = vi.hoisted(() => vi.fn());
const getMock = vi.hoisted(() => vi.fn());
const workloadsListMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: {
      resources: { list: listMock, get: getMock },
      workloads: { list: workloadsListMock },
    },
  };
});

function wrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

function emit(event: unknown) {
  act(() => {
    FakeEventSource.latest().emitMessage(JSON.stringify(event));
  });
}

let restore: () => void;
beforeEach(() => {
  restore = installFakeEventSource();
  listMock.mockReset();
  getMock.mockReset();
  workloadsListMock.mockReset();
});
afterEach(() => restore());

const podsRef = { group: "core", version: "v1", resource: "pods", namespace: "default" };

describe("useLiveResourceList", () => {
  it("patches rows in place on add/update/delete without refetching", async () => {
    const initial: ResourceList = {
      group: "core",
      version: "v1",
      resource: "pods",
      kind: "Pod",
      namespaced: true,
      columns: [],
      rows: [{ name: "a", namespace: "default", uid: "a-uid" }],
    };
    listMock.mockResolvedValue(initial);

    const { result } = renderHook(() => useLiveResourceList(podsRef), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.data?.rows).toHaveLength(1));
    act(() => FakeEventSource.latest().emitOpen());

    // Add a new row.
    emit({ type: "add", row: { name: "b", namespace: "default", uid: "b-uid" } });
    await waitFor(() => expect(result.current.data?.rows.map((r) => r.name)).toEqual(["a", "b"]));

    // Update an existing row in place (same key → replaced, not appended).
    emit({ type: "update", row: { name: "a", namespace: "default", uid: "a-uid-2" } });
    await waitFor(() => expect(result.current.data?.rows.find((r) => r.name === "a")?.uid).toBe("a-uid-2"));
    expect(result.current.data?.rows).toHaveLength(2);

    // Delete removes by identity.
    emit({ type: "delete", ref: { name: "a", namespace: "default" } });
    await waitFor(() => expect(result.current.data?.rows.map((r) => r.name)).toEqual(["b"]));

    // The list endpoint was hit exactly once — every change was a cache patch.
    expect(listMock).toHaveBeenCalledTimes(1);
  });
});

describe("useLiveWorkloadSummary", () => {
  it("patches typed summary rows in place", async () => {
    workloadsListMock.mockResolvedValue([
      { name: "web-1", namespace: "default", status: "Running", ready: "1/1" },
    ]);

    const { result } = renderHook(
      () => useLiveWorkloadSummary("pods", { group: "core", version: "v1", resource: "pods" }, "default"),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.data).toHaveLength(1));
    act(() => FakeEventSource.latest().emitOpen());

    emit({ type: "add", row: { name: "web-2", namespace: "default", status: "Pending", ready: "0/1" } });
    await waitFor(() => expect(result.current.data?.map((r) => r.name)).toEqual(["web-1", "web-2"]));

    emit({ type: "delete", ref: { name: "web-1", namespace: "default" } });
    await waitFor(() => expect(result.current.data?.map((r) => r.name)).toEqual(["web-2"]));
    expect(workloadsListMock).toHaveBeenCalledTimes(1);
  });
});

describe("useLiveResourceObject", () => {
  it("replaces the object on update and surfaces deletion", async () => {
    getMock.mockResolvedValue({ kind: "Pod", metadata: { name: "web-1", resourceVersion: "1" } } as KubeObject);

    const ref = { group: "core", version: "v1", resource: "pods", namespace: "default", name: "web-1" };
    const { result } = renderHook(() => useLiveResourceObject(ref), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.data?.metadata?.name).toBe("web-1"));
    act(() => FakeEventSource.latest().emitOpen());

    emit({
      type: "update",
      object: { kind: "Pod", metadata: { name: "web-1", resourceVersion: "2" } },
    });
    await waitFor(() => expect(result.current.data?.metadata?.resourceVersion).toBe("2"));
    expect(getMock).toHaveBeenCalledTimes(1);

    expect(result.current.deleted).toBe(false);
    emit({ type: "delete", ref: { name: "web-1", namespace: "default" } });
    await waitFor(() => expect(result.current.deleted).toBe(true));
  });
});
