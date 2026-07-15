import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { DeploymentSummary, PodSummary } from "@/lib/api";

import { ControllerDetail } from "./controller-detail";

const listMock = vi.hoisted(() => vi.fn());
const ownedPodsMock = vi.hoisted(() => vi.fn());
const ownedJobsMock = vi.hoisted(() => vi.fn());
const eventsMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: {
      workloads: { list: listMock, ownedPods: ownedPodsMock, ownedJobs: ownedJobsMock },
      events: eventsMock,
    },
  };
});

const deploymentRow: DeploymentSummary = {
  name: "web",
  namespace: "default",
  ready: "2/3",
  desiredReplicas: 3,
  readyReplicas: 2,
  updatedReplicas: 3,
  availableReplicas: 2,
  rolloutStatus: "Waiting for rollout to finish: 1 of 3 updated replicas are available",
  creationTimestamp: "2026-07-14T10:00:00Z",
};

const ownedPod: PodSummary = {
  name: "web-1",
  namespace: "default",
  ready: "1/1",
  readyContainers: 1,
  totalContainers: 1,
  status: "Running",
  phase: "Running",
  restarts: 0,
  node: "node-a",
  creationTimestamp: "2026-07-14T10:00:00Z",
};

function renderController(resource = "deployments", kind = "Deployment", name = "web", readOnly = true) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <ControllerDetail resource={resource} kind={kind} namespace="default" name={name} readOnly={readOnly} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listMock.mockReset();
  ownedPodsMock.mockReset();
  ownedJobsMock.mockReset();
  eventsMock.mockReset();
  eventsMock.mockResolvedValue([]);
  ownedJobsMock.mockResolvedValue([]);
});

describe("ControllerDetail", () => {
  it("renders replica counts and the server-computed rollout status", async () => {
    listMock.mockResolvedValue([deploymentRow]);
    ownedPodsMock.mockResolvedValue([ownedPod]);

    renderController();

    expect(await screen.findByText("2/3")).toBeInTheDocument(); // ready
    const rollout = screen.getByTestId("rollout-status");
    expect(rollout).toHaveTextContent(/1 of 3 updated replicas are available/);
  });

  it("lists owned pods, each linking to Pod detail", async () => {
    listMock.mockResolvedValue([deploymentRow]);
    ownedPodsMock.mockResolvedValue([ownedPod]);

    renderController();

    const link = await screen.findByRole("link", { name: "web-1" });
    expect(link).toHaveAttribute("href", "/resources/core/v1/pods/default/web-1");
    expect(ownedPodsMock).toHaveBeenCalledWith("deployments", "default", "web");
  });

  it("shows an empty state when the controller owns no pods", async () => {
    listMock.mockResolvedValue([deploymentRow]);
    ownedPodsMock.mockResolvedValue([]);

    renderController();
    expect(await screen.findByText(/no pods owned by this controller/i)).toBeInTheDocument();
  });

  it("shows scale and restart controls when writable", async () => {
    listMock.mockResolvedValue([deploymentRow]);
    ownedPodsMock.mockResolvedValue([ownedPod]);

    renderController("deployments", "Deployment", "web", false);

    const actions = await screen.findByTestId("controller-actions");
    expect(within(actions).getByRole("button", { name: /scale/i })).toBeInTheDocument();
    expect(within(actions).getByRole("button", { name: /restart/i })).toBeInTheDocument();
  });

  it("hides mutation controls in read-only mode", async () => {
    listMock.mockResolvedValue([deploymentRow]);
    ownedPodsMock.mockResolvedValue([ownedPod]);

    renderController("deployments", "Deployment", "web", true);

    await screen.findByText("2/3");
    expect(screen.queryByTestId("controller-actions")).not.toBeInTheDocument();
  });

  it("renders a CronJob's schedule and its owned Jobs instead of pods", async () => {
    listMock.mockResolvedValue([
      {
        name: "nightly",
        namespace: "default",
        schedule: "0 0 * * *",
        suspend: false,
        active: 1,
        lastScheduleTime: "2026-07-15T00:00:00Z",
      },
    ]);
    ownedJobsMock.mockResolvedValue([
      { name: "nightly-123", namespace: "default", completions: "1/1", succeeded: 1, failed: 0, active: 0, duration: "12s" },
    ]);

    renderController("cronjobs", "CronJob", "nightly");

    expect(await screen.findByText("0 0 * * *")).toBeInTheDocument();
    const jobsSection = screen.getByRole("region", { name: "Jobs" });
    expect(within(jobsSection).getByRole("link", { name: "nightly-123" })).toHaveAttribute(
      "href",
      "/resources/batch/v1/jobs/default/nightly-123",
    );
    expect(ownedPodsMock).not.toHaveBeenCalled();
  });
});
