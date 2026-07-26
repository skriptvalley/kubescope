import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type KubeObject } from "@/lib/api";

const startMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { portForwards: { start: startMock } } };
});

import { PortForwardControls, ServicePortForwardControls } from "./port-forward-controls";

const pod = {
  kind: "Pod",
  spec: { containers: [{ name: "app", ports: [{ containerPort: 8080 }] }] },
} as unknown as KubeObject;

function renderControls() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <PortForwardControls namespace="default" pod="web-1" object={pod} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  startMock.mockReset();
  startMock.mockResolvedValue({});
});
afterEach(() => vi.clearAllMocks());

describe("PortForwardControls", () => {
  it("prefills the pod port from a declared containerPort", () => {
    renderControls();
    expect(screen.getByLabelText("Pod port")).toHaveValue(8080);
  });

  it("starts a forward with the entered ports (blank local = auto)", async () => {
    renderControls();
    fireEvent.click(screen.getByRole("button", { name: /forward/i }));
    await waitFor(() =>
      expect(startMock).toHaveBeenCalledWith({
        namespace: "default",
        pod: "web-1",
        remotePort: 8080,
        localPort: 0,
      }),
    );
  });

  it("surfaces a start failure", async () => {
    startMock.mockRejectedValue(new ApiError("port already in use", "port_in_use", 409));
    renderControls();
    fireEvent.click(screen.getByRole("button", { name: /forward/i }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/port already in use \(port_in_use\)/i),
    );
  });
});

// FB-13: the same control, pointed at a Service — one listener load-balancing
// across the ready endpoints instead of a single pod tunnel.
function renderServiceControls(ports: number[], readyEndpoints: number) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ServicePortForwardControls
        namespace="web"
        service="frontend"
        ports={ports}
        readyEndpoints={readyEndpoints}
      />
    </QueryClientProvider>,
  );
}

describe("ServicePortForwardControls", () => {
  it("announces the load-balanced fan-out and prefills the first service port", () => {
    renderServiceControls([80, 443], 3);
    expect(screen.getByText(/load-balanced across 3 endpoints/i)).toBeInTheDocument();
    expect(screen.getByLabelText("Service port")).toHaveValue(80);
  });

  it("starts a service forward with the service port (blank local = auto)", async () => {
    renderServiceControls([80], 3);
    fireEvent.click(screen.getByRole("button", { name: /forward/i }));
    await waitFor(() =>
      expect(startMock).toHaveBeenCalledWith({
        namespace: "web",
        service: "frontend",
        servicePort: 80,
        localPort: 0,
      }),
    );
  });

  it("singularizes a one-endpoint rotation", () => {
    renderServiceControls([80], 1);
    expect(screen.getByText(/load-balanced across 1 endpoint\b/i)).toBeInTheDocument();
  });

  it("blocks the start when the service has no ready endpoints", () => {
    renderServiceControls([80], 0);
    expect(screen.getByText(/no ready endpoints to forward to/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /forward/i })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /forward/i }));
    expect(startMock).not.toHaveBeenCalled();
  });
});
