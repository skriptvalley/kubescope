import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type Discovery, type ResourceList } from "@/lib/api";

import { ResourceListPage } from "./resource-list";

const discoveryMock = vi.hoisted(() => vi.fn());
const listMock = vi.hoisted(() => vi.fn());
const namespacesMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: {
      resources: { discovery: discoveryMock, list: listMock },
      namespaces: { list: namespacesMock },
    },
  };
});

const deploymentsDiscovery: Discovery = {
  groups: [
    {
      name: "apps",
      resources: [
        {
          group: "apps",
          version: "v1",
          resource: "deployments",
          kind: "Deployment",
          namespaced: true,
          verbs: ["list"],
        },
      ],
    },
  ],
};

const nodesDiscovery: Discovery = {
  groups: [
    {
      name: "",
      resources: [
        { group: "", version: "v1", resource: "nodes", kind: "Node", namespaced: false, verbs: ["list"] },
      ],
    },
  ],
};

function deploymentList(rows: ResourceList["rows"]): ResourceList {
  return {
    group: "apps",
    version: "v1",
    resource: "deployments",
    kind: "Deployment",
    namespaced: true,
    columns: [
      { id: "name", header: "Name" },
      { id: "namespace", header: "Namespace" },
      { id: "age", header: "Age" },
    ],
    rows,
  };
}

function renderList(route: string, path = "/resources/:group/:version/:resource") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <Routes>
          <Route path={path} element={<ResourceListPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  discoveryMock.mockReset();
  listMock.mockReset();
  namespacesMock.mockReset();
  namespacesMock.mockResolvedValue(["default", "kube-system"]);
});

describe("ResourceListPage", () => {
  it("renders rows and a namespace selector for a namespaced kind", async () => {
    discoveryMock.mockResolvedValue(deploymentsDiscovery);
    listMock.mockResolvedValue(
      deploymentList([{ name: "web", namespace: "default", creationTimestamp: "2026-07-14T10:00:00Z" }]),
    );

    renderList("/resources/apps/v1/deployments");

    expect(await screen.findByRole("link", { name: "web" })).toBeInTheDocument();
    expect(await screen.findByLabelText("Namespace")).toBeInTheDocument();
    expect(screen.getByText("Deployment")).toBeInTheDocument();
  });

  it("hides the namespace selector for a cluster-scoped kind", async () => {
    discoveryMock.mockResolvedValue(nodesDiscovery);
    listMock.mockResolvedValue({
      group: "",
      version: "v1",
      resource: "nodes",
      kind: "Node",
      namespaced: false,
      columns: [
        { id: "name", header: "Name" },
        { id: "age", header: "Age" },
      ],
      rows: [{ name: "node-1", creationTimestamp: "2026-07-14T10:00:00Z" }],
    });

    renderList("/resources/core/v1/nodes");

    expect(await screen.findByRole("link", { name: "node-1" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Namespace")).not.toBeInTheDocument();
  });

  it("shows an empty state when there are no objects", async () => {
    discoveryMock.mockResolvedValue(deploymentsDiscovery);
    listMock.mockResolvedValue(deploymentList([]));

    renderList("/resources/apps/v1/deployments");
    expect(await screen.findByText(/no deployments found/i)).toBeInTheDocument();
  });

  it("surfaces a structured unknown-resource error", async () => {
    discoveryMock.mockResolvedValue({ groups: [] });
    listMock.mockRejectedValue(new ApiError("no such resource", "unknown_resource", 404));

    renderList("/resources/apps/v1/widgets");
    expect(await screen.findByText(/unknown resource/i)).toBeInTheDocument();
    expect(screen.getByText(/no such resource \(unknown_resource\)/i)).toBeInTheDocument();
  });
});
