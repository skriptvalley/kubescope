import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Overview } from "@/lib/api";
import { connectivity } from "@/lib/connectivity";

import { healthRefetchInterval, useSwitchContext } from "./use-contexts";
import { useOverview } from "./use-overview";

const switchMock = vi.hoisted(() => vi.fn());
const overviewMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { contexts: { switch: switchMock }, overview: overviewMock },
  };
});

const devOverview: Overview = { context: "dev", serverVersion: "v1.30.0", nodeCount: 1, namespaces: ["web"] };
const prodOverview: Overview = { context: "prod", serverVersion: "v1.31.0", nodeCount: 3, namespaces: ["default"] };

function wrapper() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  switchMock.mockReset().mockResolvedValue([{ name: "prod", cluster: "c", namespace: "default", active: true }]);
  overviewMock.mockReset();
});
afterEach(() => vi.restoreAllMocks());

describe("healthRefetchInterval", () => {
  afterEach(() => connectivity.resetForTests());

  it("polls every 30s when reachable, backing off to 60s when unreachable", () => {
    expect(healthRefetchInterval()).toBe(30_000);
    connectivity.setActiveUnreachable(true);
    expect(healthRefetchInterval()).toBe(60_000);
  });
});

describe("useSwitchContext", () => {
  it("refetches a mounted cluster view in place on switch (no manual refresh needed)", async () => {
    // Regression: removing the active query left the current page stranded on the
    // prior cluster's data until navigation/refresh. It must refetch in place.
    overviewMock.mockResolvedValueOnce(devOverview).mockResolvedValue(prodOverview);

    const { result } = renderHook(() => ({ overview: useOverview(), switch: useSwitchContext() }), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.overview.data?.context).toBe("dev"));

    await act(async () => {
      await result.current.switch.mutateAsync("prod");
    });

    // The overview query refetched itself — the mounted page now shows prod.
    await waitFor(() => expect(result.current.overview.data?.context).toBe("prod"));
    expect(overviewMock).toHaveBeenCalledTimes(2);
  });
});
