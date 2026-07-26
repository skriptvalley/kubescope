import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ResourceGraph from "@/components/resource-graph";
import { ApiError, type ResourceGraph as ResourceGraphData } from "@/lib/api";

const graphMock = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { ...original.api, graph: graphMock } };
});

// jsdom has no canvas, so the renderer is stubbed: this suite covers the view's
// chrome and the elements handed to Cytoscape. The DTO→elements mapping itself
// is unit-tested in lib/graph-elements.test.ts.
interface CyOptions {
  elements: { data: Record<string, unknown>; classes: string }[];
  layout: { name: string };
}
const cyInstance = vi.hoisted(() => ({ on: vi.fn(), destroy: vi.fn() }));
const cytoscapeMock = vi.hoisted(() => vi.fn<(options: CyOptions) => typeof cyInstance>(() => cyInstance));
vi.mock("cytoscape", () => ({
  default: Object.assign(cytoscapeMock, { use: vi.fn() }),
}));
vi.mock("cytoscape-fcose", () => ({ default: {} }));

const navigateMock = vi.hoisted(() => vi.fn());
vi.mock("react-router-dom", async (importOriginal) => {
  const original = await importOriginal<typeof import("react-router-dom")>();
  return { ...original, useNavigate: () => navigateMock };
});

function graph(overrides: Partial<ResourceGraphData> = {}): ResourceGraphData {
  return {
    namespace: "web",
    focus: { group: "apps", version: "v1", resource: "deployments", kind: "Deployment", namespace: "web", name: "api" },
    depth: 3,
    nodes: [
      {
        id: "apps/Deployment/web/api", group: "apps", version: "v1", resource: "deployments",
        kind: "Deployment", namespace: "web", name: "api", status: "2/2", depth: 0, focus: true,
      },
      {
        id: "core/Pod/web/api-1", group: "", version: "v1", resource: "pods",
        kind: "Pod", namespace: "web", name: "api-1", status: "Running", depth: 1,
      },
    ],
    edges: [
      {
        id: "apps/Deployment/web/api->core/Pod/web/api-1",
        source: "apps/Deployment/web/api", target: "core/Pod/web/api-1", relation: "owns",
      },
    ],
    groups: [],
    partial: false,
    ...overrides,
  };
}

function renderGraph() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ResourceGraph namespace="web" kind="Deployment" name="api" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ResourceGraph", () => {
  beforeEach(() => {
    graphMock.mockReset();
    cytoscapeMock.mockClear();
    cyInstance.on.mockReset();
    cyInstance.destroy.mockReset();
    navigateMock.mockReset();
  });

  it("shows a loading placeholder before the graph arrives", () => {
    graphMock.mockReturnValue(new Promise(() => {}));
    renderGraph();
    expect(screen.getByTestId("graph-loading")).toBeInTheDocument();
  });

  it("renders the canvas and feeds Cytoscape the mapped elements", async () => {
    graphMock.mockResolvedValue(graph());
    renderGraph();

    await waitFor(() => expect(screen.getByTestId("graph-canvas")).toBeInTheDocument());
    expect(screen.getByText(/2 objects/)).toBeInTheDocument();

    expect(cytoscapeMock).toHaveBeenCalled();
    const config = cytoscapeMock.mock.calls[0][0];
    expect(config.layout.name).toBe("fcose");
    expect(config.elements.map((e) => e.data.id)).toEqual([
      "apps/Deployment/web/api",
      "core/Pod/web/api-1",
      "apps/Deployment/web/api->core/Pod/web/api-1",
    ]);
    expect(config.elements[0].classes).toContain("focus");
  });

  it("passes compound parents through to the renderer", async () => {
    graphMock.mockResolvedValue(
      graph({
        nodes: [
          {
            id: "apps/Deployment/web/api", group: "apps", version: "v1", resource: "deployments",
            kind: "Deployment", namespace: "web", name: "api", depth: 0, focus: true,
            parent: "group/apps/Deployment/web/api",
          },
          {
            id: "core/Pod/web/api-1", group: "", version: "v1", resource: "pods",
            kind: "Pod", namespace: "web", name: "api-1", depth: 1,
            parent: "group/apps/Deployment/web/api",
          },
        ],
        groups: [{ id: "group/apps/Deployment/web/api", label: "api", kind: "Deployment", root: "apps/Deployment/web/api" }],
      }),
    );
    renderGraph();

    await waitFor(() => expect(cytoscapeMock).toHaveBeenCalled());
    const { elements } = cytoscapeMock.mock.calls[0][0];
    expect(elements[0].classes).toBe("group");
    expect(elements[1].data.parent).toBe("group/apps/Deployment/web/api");
    expect(elements[2].data.parent).toBe("group/apps/Deployment/web/api");
  });

  it("opens a node's detail view when it is clicked", async () => {
    graphMock.mockResolvedValue(graph());
    renderGraph();

    await waitFor(() => expect(cyInstance.on).toHaveBeenCalledWith("tap", "node", expect.any(Function)));
    const handler = cyInstance.on.mock.calls[0][2] as (event: { target: { data: (k: string) => unknown } }) => void;
    handler({ target: { data: () => "/resources/core/v1/pods/web/api-1" } });
    expect(navigateMock).toHaveBeenCalledWith("/resources/core/v1/pods/web/api-1");

    // A node with nowhere to go must not navigate to an empty route.
    navigateMock.mockClear();
    handler({ target: { data: () => "" } });
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it("refetches at the chosen depth", async () => {
    graphMock.mockResolvedValue(graph());
    renderGraph();

    await waitFor(() => expect(graphMock).toHaveBeenCalledWith("web", { kind: "Deployment", name: "api" }, 3));
    fireEvent.change(screen.getByLabelText("Graph depth"), { target: { value: "1" } });
    await waitFor(() => expect(graphMock).toHaveBeenCalledWith("web", { kind: "Deployment", name: "api" }, 1));
  });

  it("says why a graph is partial instead of quietly showing less", async () => {
    graphMock.mockResolvedValue(
      graph({ partial: true, notes: ["the graph hit its 150-node cap; relations beyond it are not shown"] }),
    );
    renderGraph();

    await waitFor(() => expect(screen.getByTestId("graph-partial")).toBeInTheDocument());
    expect(screen.getByText(/150-node cap/)).toBeInTheDocument();
  });

  it("explains an unconnected object rather than drawing an empty canvas", async () => {
    graphMock.mockResolvedValue(graph({ nodes: [graph().nodes[0]], edges: [] }));
    renderGraph();

    await waitFor(() => expect(screen.getByTestId("empty-state")).toBeInTheDocument());
    expect(screen.getByText(/Nothing is linked to this Deployment/)).toBeInTheDocument();
    expect(screen.queryByTestId("graph-canvas")).not.toBeInTheDocument();
  });

  it("surfaces a classified backend failure with a retry", async () => {
    graphMock.mockRejectedValue(new ApiError("no kind \"Widget\"", "unknown_resource", 404));
    renderGraph();

    await waitFor(() => expect(screen.getByTestId("error-state")).toBeInTheDocument());
    expect(screen.getByText(/unknown_resource/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("tears the renderer down on unmount", async () => {
    graphMock.mockResolvedValue(graph());
    const { unmount } = renderGraph();
    await waitFor(() => expect(cytoscapeMock).toHaveBeenCalled());
    unmount();
    expect(cyInstance.destroy).toHaveBeenCalled();
  });
});
