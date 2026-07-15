import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { KubeObject } from "@/lib/api";

import { PodDetail } from "./pod-detail";

const eventsMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { events: eventsMock } };
});

const pod = {
  kind: "Pod",
  metadata: {
    name: "web-1",
    namespace: "default",
    ownerReferences: [{ kind: "ReplicaSet", name: "web-abc", controller: true }],
  },
  spec: {
    nodeName: "node-a",
    containers: [{ name: "app", image: "nginx:1" }],
    initContainers: [{ name: "init", image: "busybox" }],
  },
  status: {
    phase: "Running",
    podIP: "10.0.0.5",
    qosClass: "Burstable",
    conditions: [
      { type: "Ready", status: "True" },
      { type: "PodScheduled", status: "True" },
    ],
    initContainerStatuses: [
      { name: "init", image: "busybox", ready: true, restartCount: 0, state: { terminated: { reason: "Completed", exitCode: 0 } } },
    ],
    containerStatuses: [
      { name: "app", image: "nginx:1", ready: false, restartCount: 4, state: { waiting: { reason: "CrashLoopBackOff" } } },
    ],
  },
} as unknown as KubeObject;

function renderPod() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <PodDetail object={pod} namespace="default" name="web-1" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  eventsMock.mockReset();
  eventsMock.mockResolvedValue([]);
});

describe("PodDetail", () => {
  it("renders placement, container states and restart counts", () => {
    renderPod();

    expect(screen.getByText("node-a")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.5")).toBeInTheDocument();
    expect(screen.getByText("Burstable")).toBeInTheDocument();

    // App container: waiting reason surfaced, restarts shown.
    expect(screen.getByText("CrashLoopBackOff")).toBeInTheDocument();
    expect(screen.getByText("restarts: 4")).toBeInTheDocument();
    // Init container: terminated reason surfaced.
    expect(screen.getByText("Completed")).toBeInTheDocument();
  });

  it("groups init and app containers separately", () => {
    renderPod();
    expect(screen.getByTestId("containers-init-containers")).toBeInTheDocument();
    expect(screen.getByTestId("containers-containers")).toBeInTheDocument();
  });

  it("links the controlling owner to its detail route", () => {
    renderPod();
    expect(screen.getByRole("link", { name: "ReplicaSet/web-abc" })).toHaveAttribute(
      "href",
      "/resources/apps/v1/replicasets/default/web-abc",
    );
  });

  it("renders conditions", () => {
    renderPod();
    const conditions = screen.getByTestId("pod-conditions");
    expect(conditions).toHaveTextContent("Ready=True");
    expect(conditions).toHaveTextContent("PodScheduled=True");
  });
});
