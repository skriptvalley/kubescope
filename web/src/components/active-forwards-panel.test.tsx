import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { PortForward } from "@/lib/api";

const listMock = vi.hoisted(() => vi.fn());
const stopMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { portForwards: { list: listMock, stop: stopMock } } };
});

import { ActiveForwardsPanel } from "./active-forwards-panel";

const POD_FORWARD: PortForward = {
  id: "pf-1",
  context: "kind-dev",
  namespace: "web",
  targetKind: "pod",
  pod: "frontend-a",
  localPort: 15000,
  remotePort: 80,
  startedAt: new Date().toISOString(),
};

const SERVICE_FORWARD: PortForward = {
  id: "pf-2",
  context: "kind-dev",
  namespace: "web",
  targetKind: "service",
  service: "frontend",
  backends: 3,
  localPort: 15001,
  remotePort: 80,
  startedAt: new Date().toISOString(),
};

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ActiveForwardsPanel />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listMock.mockReset();
  stopMock.mockReset();
  stopMock.mockResolvedValue(undefined);
});
afterEach(() => vi.clearAllMocks());

describe("ActiveForwardsPanel", () => {
  it("renders nothing when there are no forwards", async () => {
    listMock.mockResolvedValue([]);
    renderPanel();
    await waitFor(() => expect(listMock).toHaveBeenCalled());
    expect(screen.queryByLabelText("Active port-forwards")).not.toBeInTheDocument();
  });

  it("shows a pod forward by pod name", async () => {
    listMock.mockResolvedValue([POD_FORWARD]);
    renderPanel();
    expect(await screen.findByText("127.0.0.1:15000 → web/frontend-a:80")).toBeInTheDocument();
  });

  it("shows a service forward with its name and live backend count", async () => {
    listMock.mockResolvedValue([SERVICE_FORWARD]);
    renderPanel();
    expect(await screen.findByText("127.0.0.1:15001 → web/frontend:80")).toBeInTheDocument();
    expect(screen.getByText("3 endpoints")).toBeInTheDocument();
  });

  it("singularizes a rotation that has shrunk to one backend", async () => {
    listMock.mockResolvedValue([{ ...SERVICE_FORWARD, backends: 1 }]);
    renderPanel();
    expect(await screen.findByText("1 endpoint")).toBeInTheDocument();
  });

  it("stops a service forward the same way as a pod one", async () => {
    listMock.mockResolvedValue([POD_FORWARD, SERVICE_FORWARD]);
    renderPanel();

    const stopService = await screen.findByRole("button", {
      name: "Stop forward to web/frontend:80",
    });
    fireEvent.click(stopService);
    await waitFor(() => expect(stopMock).toHaveBeenCalledWith("pf-2"));

    fireEvent.click(screen.getByRole("button", { name: "Stop forward to web/frontend-a:80" }));
    await waitFor(() => expect(stopMock).toHaveBeenCalledWith("pf-1"));
  });
});
