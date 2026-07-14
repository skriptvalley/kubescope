import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type KubeObject } from "@/lib/api";

import { ResourceDetailPage } from "./resource-detail";

const getMock = vi.hoisted(() => vi.fn());
const yamlMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { resources: { get: getMock, yaml: yamlMock } },
  };
});

const deployment: KubeObject = {
  apiVersion: "apps/v1",
  kind: "Deployment",
  metadata: {
    name: "web",
    namespace: "default",
    creationTimestamp: "2026-07-14T10:00:00Z",
    labels: { app: "web" },
    annotations: { "deployment.kubernetes.io/revision": "3" },
    ownerReferences: [{ kind: "ReplicaSet", name: "web-abc", controller: true }],
  },
};

function renderDetail(route = "/resources/apps/v1/deployments/default/web") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <Routes>
          <Route
            path="/resources/:group/:version/:resource/:namespace/:name"
            element={<ResourceDetailPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  getMock.mockReset();
  yamlMock.mockReset();
});

describe("ResourceDetailPage", () => {
  it("renders generic metadata in the summary tab", async () => {
    getMock.mockResolvedValue(deployment);

    renderDetail();

    // Wait for the object body to load (the title renders from the route param
    // immediately, so key on a field that only appears once metadata arrives).
    expect(await screen.findByText("app=web")).toBeInTheDocument();
    expect(screen.getByText("web")).toBeInTheDocument(); // title
    expect(screen.getAllByText(/Deployment/).length).toBeGreaterThan(0);
    expect(screen.getByText("deployment.kubernetes.io/revision")).toBeInTheDocument();
    expect(screen.getByText("ReplicaSet/web-abc")).toBeInTheDocument();
  });

  it("loads YAML lazily when the YAML tab is opened", async () => {
    getMock.mockResolvedValue(deployment);
    yamlMock.mockResolvedValue("kind: Deployment\nmetadata:\n  name: web");

    renderDetail();
    await screen.findByText("app=web"); // summary loaded; YAML not fetched yet
    expect(yamlMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("tab", { name: "YAML" }));

    const view = await screen.findByTestId("yaml-view");
    expect(view.textContent).toContain("kind: Deployment");
    expect(yamlMock).toHaveBeenCalledTimes(1);
  });

  it("renders a not-found error state", async () => {
    getMock.mockRejectedValue(new ApiError("gone", "not_found", 404));

    renderDetail();
    expect(await screen.findByText("Not found")).toBeInTheDocument();
    expect(screen.getByText(/gone \(not_found\)/i)).toBeInTheDocument();
  });
});
