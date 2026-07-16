import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ServiceDetail } from "@/components/service-detail";
import { ApiError, type ServiceDetail as ServiceDetailData } from "@/lib/api";

const detailMock = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { ...original.api, services: { detail: detailMock } } };
});

function renderDetail() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <ServiceDetail namespace="default" name="web" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const DETAIL: ServiceDetailData = {
  name: "web",
  namespace: "default",
  type: "ClusterIP",
  clusterIP: "10.0.0.5",
  selector: { app: "web" },
  ports: [{ name: "http", port: 80, protocol: "TCP", targetPort: "8080" }],
  endpointsFound: true,
  readyAddresses: [
    { ip: "10.1.0.1", ready: true, targetRef: { kind: "Pod", namespace: "default", name: "web-1" } },
  ],
  notReadyAddresses: [
    { ip: "10.1.0.2", ready: false, targetRef: { kind: "Pod", namespace: "default", name: "web-2" } },
  ],
};

describe("ServiceDetail", () => {
  beforeEach(() => {
    detailMock.mockReset();
  });

  it("renders the selector, ports and ready/not-ready endpoints with pod links", async () => {
    detailMock.mockResolvedValue(DETAIL);
    renderDetail();

    expect(await screen.findByText("app=web")).toBeInTheDocument();
    expect(screen.getByText("Ready")).toBeInTheDocument();
    expect(screen.getByText("Not ready")).toBeInTheDocument();

    // Each backing address links to its pod detail (the matching pod list).
    expect(screen.getByRole("link", { name: "web-1" })).toHaveAttribute(
      "href",
      "/resources/core/v1/pods/default/web-1",
    );
    expect(screen.getByRole("link", { name: "web-2" })).toHaveAttribute(
      "href",
      "/resources/core/v1/pods/default/web-2",
    );
  });

  it("shows an empty state when there are no endpoints", async () => {
    detailMock.mockResolvedValue({
      ...DETAIL,
      endpointsFound: false,
      readyAddresses: [],
      notReadyAddresses: [],
    });
    renderDetail();
    expect(await screen.findByText(/no endpoints/i)).toBeInTheDocument();
  });

  it("shows a retryable error state on failure", async () => {
    detailMock.mockRejectedValue(new ApiError("gone", "not_found", 404));
    renderDetail();
    expect(await screen.findByTestId("error-state")).toHaveTextContent("Not found");
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });
});
