import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type Discovery, type SetupState } from "@/lib/api";

import { Sidebar } from "./sidebar";

const discoveryMock = vi.hoisted(() => vi.fn());
const setupMock = vi.hoisted(() => vi.fn());
const countsMock = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/use-discovery", () => ({ useDiscovery: discoveryMock }));
vi.mock("@/hooks/use-setup", () => ({ useSetupState: setupMock }));
vi.mock("@/hooks/use-counts", () => ({ useResourceCounts: countsMock }));

const DISCOVERY: Discovery = {
  groups: [
    {
      name: "",
      resources: [
        { group: "", version: "v1", resource: "pods", kind: "Pod", namespaced: true, verbs: ["list"] },
      ],
    },
  ],
};

function setupState(state: SetupState["state"]): { data: SetupState } {
  return { data: { state, kubeconfigSources: ["/kubeconfig"], canSetKubeconfig: false } };
}

function renderSidebar() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/overview"]}>
        <Sidebar />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  discoveryMock.mockReset();
  setupMock.mockReset();
  countsMock.mockReset().mockReturnValue({ data: undefined }); // no counts by default
});

describe("Sidebar", () => {
  it("shows a muted placeholder (not a red error) when discovery fails before the server is ready", () => {
    discoveryMock.mockReturnValue({
      data: undefined,
      isPending: false,
      isError: true,
      error: new ApiError("discovery failed", "cluster_unreachable", 502),
    });
    setupMock.mockReturnValue(setupState("active_unreachable"));

    renderSidebar();

    expect(screen.getByText(/waiting for a cluster connection/i)).toBeInTheDocument();
    expect(screen.queryByText("discovery failed")).toBeNull();
  });

  it("shows the red discovery error when it fails while the server is ready", () => {
    discoveryMock.mockReturnValue({
      data: undefined,
      isPending: false,
      isError: true,
      error: new ApiError("discovery failed", "cluster_unreachable", 502),
    });
    setupMock.mockReturnValue(setupState("ready"));

    renderSidebar();

    expect(screen.getByText("discovery failed")).toBeInTheDocument();
    expect(screen.queryByText(/waiting for a cluster connection/i)).toBeNull();
  });

  it("renders the discovered nav when ready", () => {
    discoveryMock.mockReturnValue({
      data: DISCOVERY,
      isPending: false,
      isError: false,
      error: null,
    });
    setupMock.mockReturnValue(setupState("ready"));

    renderSidebar();

    // Pinned items plus the discovered resource kind, no error placeholder.
    expect(screen.getByText("Overview")).toBeInTheDocument();
    expect(screen.getByText("Pod")).toBeInTheDocument();
    expect(screen.queryByText(/waiting for a cluster connection/i)).toBeNull();
  });

  it("renders a per-type count beside a nav item, keyed by group/version/resource", () => {
    discoveryMock.mockReturnValue({ data: DISCOVERY, isPending: false, isError: false, error: null });
    setupMock.mockReturnValue(setupState("ready"));
    // Nav key for core pods is "/v1/pods" (empty group), matching the backend.
    countsMock.mockReturnValue({ data: { counts: { "/v1/pods": 12 }, partial: false } });

    renderSidebar();

    expect(screen.getByText("Pod")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
  });

  it("renders no count when counts are unavailable", () => {
    discoveryMock.mockReturnValue({ data: DISCOVERY, isPending: false, isError: false, error: null });
    setupMock.mockReturnValue(setupState("ready"));
    countsMock.mockReturnValue({ data: undefined });

    renderSidebar();

    expect(screen.getByText("Pod")).toBeInTheDocument();
    expect(screen.queryByText("12")).toBeNull();
  });
});
