import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { CronJobSummary, PodSummary } from "@/lib/api";

import { WorkloadListPage } from "./workload-list";

const listMock = vi.hoisted(() => vi.fn());
const namespacesMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { workloads: { list: listMock }, namespaces: { list: namespacesMock } },
  };
});

const pods: PodSummary[] = [
  {
    name: "web-1",
    namespace: "default",
    ready: "1/2",
    readyContainers: 1,
    totalContainers: 2,
    status: "CrashLoopBackOff",
    phase: "Running",
    restarts: 7,
    node: "node-a",
    creationTimestamp: "2026-07-14T10:00:00Z",
  },
];

function renderWorkloadList(
  props = { group: "core", version: "v1", resource: "pods", kind: "Pod" },
) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <WorkloadListPage {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listMock.mockReset();
  namespacesMock.mockReset();
  namespacesMock.mockResolvedValue(["default", "kube-system"]);
});

describe("WorkloadListPage", () => {
  it("renders pod-specific columns: ready, status, restarts and node", async () => {
    listMock.mockResolvedValue(pods);

    renderWorkloadList();

    const nameLink = await screen.findByRole("link", { name: "web-1" });
    expect(nameLink).toHaveAttribute("href", "/resources/core/v1/pods/default/web-1");

    // Kind-specific headers.
    expect(screen.getByRole("button", { name: "Sort by Status" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sort by Restarts" })).toBeInTheDocument();

    const row = nameLink.closest("tr")!;
    expect(within(row).getByText("1/2")).toBeInTheDocument(); // ready
    expect(within(row).getByText("CrashLoopBackOff")).toBeInTheDocument(); // status badge
    expect(within(row).getByText("7")).toBeInTheDocument(); // restarts
    expect(within(row).getByText("node-a")).toBeInTheDocument();
  });

  it("requests the typed summary for the kind and namespace", async () => {
    listMock.mockResolvedValue(pods);
    renderWorkloadList();
    await screen.findByRole("link", { name: "web-1" });
    expect(listMock).toHaveBeenCalledWith("pods", undefined);
  });

  it("renders cronjob-specific columns", async () => {
    const cronjobs: CronJobSummary[] = [
      {
        name: "nightly",
        namespace: "default",
        schedule: "*/5 * * * *",
        suspend: false,
        active: 0,
        lastScheduleTime: "2026-07-15T00:00:00Z",
      },
    ];
    listMock.mockResolvedValue(cronjobs);

    renderWorkloadList({ group: "batch", version: "v1", resource: "cronjobs", kind: "CronJob" });

    expect(await screen.findByText("*/5 * * * *")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sort by Schedule" })).toBeInTheDocument();
  });

  it("shows an empty state when there are no rows", async () => {
    listMock.mockResolvedValue([]);
    renderWorkloadList();
    expect(await screen.findByText(/no pods found/i)).toBeInTheDocument();
  });
});
