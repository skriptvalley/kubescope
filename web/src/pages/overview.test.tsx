import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type PodSummary } from "@/lib/api";

import { OverviewPage } from "./overview";

const useOverviewMock = vi.hoisted(() => vi.fn());
const usePodsMock = vi.hoisted(() => vi.fn());
const useNodesMock = vi.hoisted(() => vi.fn());
const useMetricsMock = vi.hoisted(() => vi.fn());

vi.mock("@/hooks/use-overview", () => ({ useOverview: useOverviewMock }));
vi.mock("@/hooks/use-stream", () => ({ useLiveWorkloadSummary: usePodsMock }));
vi.mock("@/hooks/use-nodes", () => ({ useNodes: useNodesMock }));
vi.mock("@/hooks/use-metrics", () => ({ usePodMetrics: useMetricsMock }));

const runningPod: PodSummary = {
  name: "web-1",
  namespace: "default",
  ready: "1/1",
  readyContainers: 1,
  totalContainers: 1,
  status: "Running",
  phase: "Running",
  restarts: 0,
  node: "node-a",
  creationTimestamp: "2026-07-20T00:00:00Z",
};
const failingPod: PodSummary = {
  name: "payments-api-x",
  namespace: "payments",
  ready: "0/1",
  readyContainers: 0,
  totalContainers: 1,
  status: "CrashLoopBackOff",
  phase: "Running",
  restarts: 17,
  node: "node-b",
  creationTimestamp: "2026-07-20T00:00:00Z",
};

function renderPage() {
  return render(
    <MemoryRouter>
      <OverviewPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  useOverviewMock.mockReset().mockReturnValue({
    data: { context: "prod", serverVersion: "v1.33.0", nodeCount: 3, namespaces: ["default", "kube-system", "payments"] },
    isError: false,
    isFetching: false,
    refetch: vi.fn(),
  });
  usePodsMock.mockReset().mockReturnValue({
    data: [runningPod, failingPod],
    isPending: false,
    isError: false,
    streamStatus: "live",
    refetch: vi.fn(),
    isFetching: false,
  });
  useNodesMock.mockReset().mockReturnValue({
    data: [
      { name: "node-a", status: "Ready", version: "v1.33.0", unschedulable: false },
      { name: "node-b", status: "Ready", version: "v1.33.0", unschedulable: false },
      { name: "node-c", status: "Ready", version: "v1.33.0", unschedulable: false },
    ],
    refetch: vi.fn(),
  });
  useMetricsMock.mockReset().mockReturnValue({
    available: true,
    byPod: new Map([["default/web-1", { name: "web-1", namespace: "default", cpu: "12m", memory: "96Mi" }]]),
    query: { refetch: vi.fn() },
  });
});

describe("OverviewPage", () => {
  it("renders the title, context/version subtitle and stat cards", () => {
    renderPage();
    expect(screen.getByText("Cluster overview")).toBeInTheDocument();
    expect(screen.getByText("prod")).toBeInTheDocument();
    expect(screen.getByText("v1.33.0")).toBeInTheDocument();
    expect(screen.getByText("3 Ready")).toBeInTheDocument(); // Nodes stat
    expect(screen.getByText("1 Running")).toBeInTheDocument(); // Pods stat
    expect(screen.getByText("Degraded")).toBeInTheDocument(); // Health, one failing pod
  });

  it("shows the attention banner and live pod rows with merged metrics", () => {
    renderPage();
    expect(screen.getByText(/workload failing/i)).toBeInTheDocument();
    expect(screen.getByText("web-1")).toBeInTheDocument();
    expect(screen.getAllByText("payments-api-x").length).toBeGreaterThan(0); // banner + row
    expect(screen.getAllByText("CrashLoopBackOff").length).toBeGreaterThan(0); // banner + status badge
    // metrics-server usage merged into the CPU/Memory columns.
    expect(screen.getByText("12m")).toBeInTheDocument();
    expect(screen.getByText("96Mi")).toBeInTheDocument();
  });

  it("shows a loading skeleton while the pods query is pending", () => {
    usePodsMock.mockReturnValue({
      data: undefined,
      isPending: true,
      isError: false,
      streamStatus: "connecting",
      refetch: vi.fn(),
      isFetching: true,
    });
    renderPage();
    expect(screen.getByTestId("overview-pods-loading")).toBeInTheDocument();
  });

  it("renders a clear error state when the cluster is unreachable", () => {
    useOverviewMock.mockReturnValue({
      data: undefined,
      isError: true,
      error: new ApiError("listing nodes: connection refused", "cluster_unreachable", 502),
      isFetching: false,
      refetch: vi.fn(),
    });
    renderPage();
    expect(screen.getByText(/cluster unreachable/i)).toBeInTheDocument();
    expect(screen.getByText(/connection refused \(cluster_unreachable\)/i)).toBeInTheDocument();
  });

  it("distinguishes a kubeconfig error and surfaces ADR-0004 guidance", () => {
    useOverviewMock.mockReturnValue({
      data: undefined,
      isError: true,
      error: new ApiError(
        "no active context",
        "kubeconfig_unavailable",
        503,
        "mount ~/.aws or pre-generate a token — see ADR-0004",
      ),
      isFetching: false,
      refetch: vi.fn(),
    });
    renderPage();
    expect(screen.getByText(/kubeconfig unavailable/i)).toBeInTheDocument();
    expect(screen.getByText(/ADR-0004/i)).toBeInTheDocument();
  });
});
