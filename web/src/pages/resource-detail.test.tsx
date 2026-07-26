import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Link, MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type KubeObject } from "@/lib/api";

import { ResourceDetailPage } from "./resource-detail";

const getMock = vi.hoisted(() => vi.fn());
const yamlMock = vi.hoisted(() => vi.fn());
const graphMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { resources: { get: getMock, yaml: yamlMock }, graph: graphMock },
  };
});

// An unregistered kind (a CRD) exercises the generic Summary tab; workload kinds
// and the Sprint 7 config/networking/RBAC/storage kinds render dedicated views
// tested elsewhere.
const configmap: KubeObject = {
  apiVersion: "example.com/v1",
  kind: "Widget",
  metadata: {
    name: "app-config",
    namespace: "default",
    creationTimestamp: "2026-07-14T10:00:00Z",
    labels: { app: "web" },
    annotations: { "kubescope.io/note": "3" },
    ownerReferences: [{ kind: "CustomResource", name: "owner-abc", controller: true }],
  },
};

function routedDetail(route: string, jumpTo?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        {/* Stands in for a graph node click: navigates within the same mounted
            tree, so the page keeps its state exactly as it would in the app. */}
        {jumpTo && <Link to={jumpTo}>jump</Link>}
        <Routes>
          <Route
            path="/resources/:group/:version/:resource/:namespace/:name"
            element={<ResourceDetailPage />}
          />
          <Route path="/resources/:group/:version/:resource/:name" element={<ResourceDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

function renderDetail(route = "/resources/example.com/v1/widgets/default/app-config") {
  return render(routedDetail(route));
}

beforeEach(() => {
  getMock.mockReset();
  yamlMock.mockReset();
  graphMock.mockReset();
  graphMock.mockReturnValue(new Promise(() => {})); // the graph never resolves; the tab state is what matters
});

describe("ResourceDetailPage", () => {
  it("renders generic metadata in the summary tab", async () => {
    getMock.mockResolvedValue(configmap);

    renderDetail();

    // Wait for the object body to load (the title renders from the route param
    // immediately, so key on a field that only appears once metadata arrives).
    expect(await screen.findByText("app=web")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "app-config" })).toBeInTheDocument(); // title
    expect(screen.getAllByText(/Widget/).length).toBeGreaterThan(0);
    expect(screen.getByText("kubescope.io/note")).toBeInTheDocument();
    expect(screen.getByText("CustomResource/owner-abc")).toBeInTheDocument();
  });

  it("loads YAML lazily when the YAML tab is opened", async () => {
    getMock.mockResolvedValue(configmap);
    yamlMock.mockResolvedValue("kind: ConfigMap\nmetadata:\n  name: app-config");

    renderDetail();
    await screen.findByText("app=web"); // summary loaded; YAML not fetched yet
    expect(yamlMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("tab", { name: "YAML" }));

    const view = await screen.findByTestId("yaml-view");
    expect(view.textContent).toContain("kind: ConfigMap");
    expect(yamlMock).toHaveBeenCalledTimes(1);
  });

  it("omits the namespace when the object is cluster-scoped (4-segment route)", async () => {
    getMock.mockResolvedValue({ kind: "Node", metadata: { name: "node-1" } });

    renderDetail("/resources/core/v1/nodes/node-1");

    await waitFor(() =>
      expect(getMock).toHaveBeenCalledWith(
        expect.objectContaining({ resource: "nodes", name: "node-1", namespace: undefined }),
      ),
    );
  });

  it("offers the relationship graph on a namespaced object (FB-14)", async () => {
    getMock.mockResolvedValue(configmap);

    renderDetail();
    await screen.findByText("app=web");
    expect(screen.getByRole("tab", { name: "Graph" })).toBeInTheDocument();
  });

  it("hides the graph tab for a cluster-scoped object, which has no namespace to scope it", async () => {
    getMock.mockResolvedValue({ kind: "Node", metadata: { name: "node-1" } });

    renderDetail("/resources/core/v1/nodes/node-1");
    await waitFor(() => expect(getMock).toHaveBeenCalled());
    expect(screen.queryByRole("tab", { name: "Graph" })).not.toBeInTheDocument();
  });

  it("falls back to Summary when the route changes to a kind without the open tab", async () => {
    getMock.mockResolvedValue(configmap);
    render(
      routedDetail(
        "/resources/example.com/v1/widgets/default/app-config",
        "/resources/core/v1/persistentvolumes/pvc-9",
      ),
    );
    await screen.findByText("app=web");
    fireEvent.click(screen.getByRole("tab", { name: "Graph" }));
    expect(screen.getByRole("tab", { name: "Graph" })).toHaveAttribute("aria-selected", "true");

    // Clicking a cluster-scoped node in the graph swaps the route under the
    // still-mounted page. The Graph tab is gone, so Summary must take over
    // rather than leaving content on screen with nothing highlighted.
    getMock.mockResolvedValue({ kind: "PersistentVolume", metadata: { name: "pvc-9" } });
    fireEvent.click(screen.getByRole("link", { name: "jump" }));

    await waitFor(() => expect(screen.queryByRole("tab", { name: "Graph" })).not.toBeInTheDocument());
    expect(screen.getByRole("tab", { name: "Summary" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByTestId("resource-graph")).not.toBeInTheDocument();
  });

  it("renders a not-found error state", async () => {
    getMock.mockRejectedValue(new ApiError("gone", "not_found", 404));

    renderDetail();
    expect(await screen.findByText("Not found")).toBeInTheDocument();
    expect(screen.getByText(/gone \(not_found\)/i)).toBeInTheDocument();
  });
});
