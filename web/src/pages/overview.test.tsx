import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ApiError, type Overview } from "@/lib/api";

import { OverviewPage } from "./overview";

const overviewMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { overview: overviewMock },
  };
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <OverviewPage />
    </QueryClientProvider>,
  );
}

describe("OverviewPage", () => {
  it("shows a loading skeleton while the query is pending", () => {
    overviewMock.mockReturnValue(new Promise<Overview>(() => {}));
    renderPage();
    expect(screen.getByTestId("overview-loading")).toBeInTheDocument();
  });

  it("renders server version, node count and namespaces", async () => {
    overviewMock.mockResolvedValue({
      context: "prod",
      serverVersion: "v1.33.0",
      nodeCount: 3,
      namespaces: ["default", "kube-system"],
    } satisfies Overview);
    renderPage();

    expect(await screen.findByText("v1.33.0")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText("default")).toBeInTheDocument();
    expect(screen.getByText("kube-system")).toBeInTheDocument();
    expect(screen.getByText(/context prod/i)).toBeInTheDocument();
  });

  it("renders a clear error state when the cluster is unreachable", async () => {
    overviewMock.mockRejectedValue(
      new ApiError("listing nodes: connection refused", "cluster_unreachable", 502),
    );
    renderPage();

    expect(await screen.findByText(/cluster unreachable/i)).toBeInTheDocument();
    expect(
      screen.getByText(/connection refused \(cluster_unreachable\)/i),
    ).toBeInTheDocument();
  });
});
