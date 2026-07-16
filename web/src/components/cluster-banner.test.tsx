import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { connectivity } from "@/lib/connectivity";

import { ClusterBanner } from "./cluster-banner";

vi.mock("@/hooks/use-contexts", () => ({
  useContexts: () => ({ data: [{ name: "prod", cluster: "c", namespace: "default", active: true }] }),
  useContextsHealth: () => ({ data: [] }),
}));

function renderBanner() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ClusterBanner />
    </QueryClientProvider>,
  );
}

afterEach(() => connectivity.resetForTests());

describe("ClusterBanner", () => {
  it("stays hidden before the first successful connection (the starter owns first-run)", () => {
    connectivity.setActiveUnreachable(true);
    renderBanner();
    expect(screen.queryByTestId("cluster-banner")).toBeNull();
  });

  it("shows for a mid-session outage once ever-connected, and hides on recovery", () => {
    connectivity.markEverConnected();
    connectivity.setActiveUnreachable(true);
    const { rerender } = renderBanner();
    expect(screen.getByTestId("cluster-banner")).toBeInTheDocument();

    connectivity.setActiveUnreachable(false);
    rerender(
      <QueryClientProvider client={new QueryClient()}>
        <ClusterBanner />
      </QueryClientProvider>,
    );
    expect(screen.queryByTestId("cluster-banner")).toBeNull();
  });
});
