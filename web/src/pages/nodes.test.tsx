import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ApiError, type NodeSummary } from "@/lib/api";

import { NodesPage } from "./nodes";

const listMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { nodes: { list: listMock } },
  };
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <NodesPage />
    </QueryClientProvider>,
  );
}

describe("NodesPage", () => {
  it("shows a loading skeleton while the query is pending", () => {
    listMock.mockReturnValue(new Promise<NodeSummary[]>(() => {}));
    renderPage();
    expect(screen.getByTestId("nodes-loading")).toBeInTheDocument();
  });

  it("renders a table row per node", async () => {
    listMock.mockResolvedValue([
      { name: "cp-1", status: "Ready", version: "v1.31.0" },
      { name: "worker-1", status: "NotReady", version: "v1.30.2" },
    ]);
    renderPage();

    expect(await screen.findByText("cp-1")).toBeInTheDocument();
    expect(screen.getByText("worker-1")).toBeInTheDocument();
    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByText("NotReady")).toBeInTheDocument();
    expect(screen.getByText("v1.31.0")).toBeInTheDocument();
  });

  it("shows the empty state for a cluster without nodes", async () => {
    listMock.mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText(/no nodes found/i)).toBeInTheDocument();
  });

  it("surfaces structured API errors", async () => {
    listMock.mockRejectedValue(
      new ApiError("cannot load kubeconfig", "kubeconfig_unavailable", 503),
    );
    renderPage();

    expect(await screen.findByText(/failed to load nodes/i)).toBeInTheDocument();
    expect(
      screen.getByText(/cannot load kubeconfig \(kubeconfig_unavailable\)/i),
    ).toBeInTheDocument();
  });
});
