import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type KubeconfigSourceList } from "@/lib/api";
import { clusterScopedKeyPrefixes } from "@/lib/query-keys";

import { KubeconfigSources } from "./kubeconfig-sources";

const listMock = vi.hoisted(() => vi.fn());
const addMock = vi.hoisted(() => vi.fn());
const removeMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { kubeconfigs: { list: listMock, add: addMock, remove: removeMock } },
  };
});

const LISTING: KubeconfigSourceList = {
  sources: [
    {
      id: "dir123",
      path: "/kubeconfigs",
      kind: "dir",
      origin: "env",
      status: "ok",
      files: [
        { path: "/kubeconfigs/a.yaml", status: "ok", contexts: ["kind-a"] },
        { path: "/kubeconfigs/broken.yaml", status: "unparseable", message: "yaml: line 2: mapping" },
      ],
      contexts: ["kind-a"],
    },
    {
      id: "file456",
      path: "/home/user/.kube/config",
      kind: "file",
      origin: "runtime",
      status: "ok",
      shadowed: ["kind-a"],
    },
  ],
  canSetKubeconfig: true,
};

function renderSources() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <KubeconfigSources />
    </QueryClientProvider>,
  );
  return { queryClient, ...utils };
}

beforeEach(() => {
  listMock.mockReset().mockResolvedValue(LISTING);
  addMock.mockReset().mockResolvedValue({ sources: [], canSetKubeconfig: true });
  removeMock.mockReset().mockResolvedValue({ sources: [], canSetKubeconfig: true });
});

describe("KubeconfigSources", () => {
  it("renders each source with kind/status/origin, its per-file rows, and the shadowed note", async () => {
    renderSources();

    expect(await screen.findByText("/kubeconfigs")).toBeInTheDocument();
    expect(screen.getByText("/home/user/.kube/config")).toBeInTheDocument();
    // Kind + origin surfaced per source.
    expect(screen.getByText("dir")).toBeInTheDocument();
    expect(screen.getByText("file")).toBeInTheDocument();
    expect(screen.getByText("env")).toBeInTheDocument();
    expect(screen.getByText("runtime")).toBeInTheDocument();
    // Per-file sub-rows for the directory source, including the broken file's message.
    expect(screen.getByText("/kubeconfigs/a.yaml")).toBeInTheDocument();
    expect(screen.getByText("/kubeconfigs/broken.yaml")).toBeInTheDocument();
    expect(screen.getByText(/yaml: line 2/)).toBeInTheDocument();
    // Shadowed note on the file source.
    expect(screen.getByText(/shadowed by an earlier source: kind-a/)).toBeInTheDocument();
  });

  it("adds a source, refetches every mounted view and drops inactive cluster caches", async () => {
    // A source mutation can repoint the ACTIVE context at a different cluster,
    // so it must mirror the context-switch pattern (FB-2): global invalidate
    // first (refetches mounted views in place), then remove only the inactive
    // cluster-scoped caches.
    const { queryClient } = renderSources();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const removeSpy = vi.spyOn(queryClient, "removeQueries");

    fireEvent.change(await screen.findByLabelText(/absolute kubeconfig path/i), {
      target: { value: "/extra/config" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    await waitFor(() => expect(addMock).toHaveBeenCalledWith("/extra/config"));
    await waitFor(() => expect(invalidateSpy).toHaveBeenCalledWith());
    const removeOrder = invalidateSpy.mock.invocationCallOrder[0];
    for (const key of clusterScopedKeyPrefixes) {
      expect(removeSpy).toHaveBeenCalledWith({ queryKey: key, type: "inactive" });
    }
    // Invalidate must run before any removal: a removed query has no observer
    // to refetch, which would strand the current page on stale data.
    expect(removeOrder).toBeLessThan(removeSpy.mock.invocationCallOrder[0]);
  });

  it("surfaces the guidance on a rejected add", async () => {
    addMock.mockRejectedValue(
      new ApiError(
        "path not visible",
        "kubeconfig_invalid",
        422,
        "mount a directory once (-v ~/.kube:/kubeconfigs:ro)",
      ),
    );
    renderSources();

    fireEvent.change(await screen.findByLabelText(/absolute kubeconfig path/i), {
      target: { value: "/missing" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^add$/i }));

    expect(await screen.findByText(/path not visible/i)).toBeInTheDocument();
    expect(screen.getByText(/mount a directory once/i)).toBeInTheDocument();
  });

  it("removes a source by id", async () => {
    renderSources();

    fireEvent.click(await screen.findByRole("button", { name: "Remove /kubeconfigs" }));
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith("dir123"));
  });

  it("rescans by refetching every mounted view", async () => {
    const { queryClient } = renderSources();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    fireEvent.click(await screen.findByRole("button", { name: /rescan/i }));

    expect(invalidateSpy).toHaveBeenCalledWith();
  });

  it("hides the add/remove controls when the registry is locked", async () => {
    listMock.mockResolvedValue({ ...LISTING, canSetKubeconfig: false });
    renderSources();

    expect(await screen.findByTestId("kubeconfig-sources-disabled")).toBeInTheDocument();
    expect(screen.queryByTestId("add-kubeconfig-source-form")).toBeNull();
    expect(screen.queryByRole("button", { name: /^Remove / })).toBeNull();
  });
});
