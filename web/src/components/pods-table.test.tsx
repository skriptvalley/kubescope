import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import type { PodMetrics, PodSummary } from "@/lib/api";

import { PodsTable } from "./pods-table";

const navigateMock = vi.hoisted(() => vi.fn());
vi.mock("react-router-dom", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-router-dom")>();
  return { ...actual, useNavigate: () => navigateMock };
});

const running: PodSummary = {
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
const pending: PodSummary = {
  name: "worker-1",
  namespace: "batch",
  ready: "0/1",
  readyContainers: 0,
  totalContainers: 1,
  status: "Pending",
  phase: "Pending",
  restarts: 0,
  node: "",
  creationTimestamp: "2026-07-20T00:00:00Z",
};

const metrics = new Map<string, PodMetrics>([
  ["default/web-1", { name: "web-1", namespace: "default", cpu: "12m", memory: "96Mi" }],
]);

function renderTable(props: Partial<React.ComponentProps<typeof PodsTable>> = {}) {
  return render(
    <MemoryRouter>
      <PodsTable pods={[running, pending]} {...props} />
    </MemoryRouter>,
  );
}

describe("PodsTable", () => {
  it("renders a row per pod with its status", () => {
    renderTable();
    expect(screen.getByText("web-1")).toBeInTheDocument();
    expect(screen.getByText("worker-1")).toBeInTheDocument();
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("Pending")).toBeInTheDocument();
  });

  it("merges metrics by namespace/name and renders '—' when a pod has none", () => {
    renderTable({ metrics, showCpuMem: true });
    expect(screen.getByText("12m")).toBeInTheDocument();
    expect(screen.getByText("96Mi")).toBeInTheDocument();
    // worker-1 has no metrics entry → both CPU and Memory render "—".
    expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(2);
  });

  it("renders the empty message when there are no pods", () => {
    render(
      <MemoryRouter>
        <PodsTable pods={[]} emptyMessage="No pods here." />
      </MemoryRouter>,
    );
    expect(screen.getByText("No pods here.")).toBeInTheDocument();
  });

  it("navigates to the pod detail route on row click", () => {
    navigateMock.mockClear();
    renderTable();
    fireEvent.click(screen.getByText("web-1"));
    expect(navigateMock).toHaveBeenCalledWith("/resources/core/v1/pods/default/web-1");
  });

  it("links the namespace cell to the namespace detail when showNamespace", () => {
    renderTable({ showNamespace: true });
    const nsLink = screen.getByRole("link", { name: "default" });
    expect(nsLink).toHaveAttribute("href", "/resources/core/v1/namespaces/default");
  });
});
