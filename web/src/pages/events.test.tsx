import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { EventFeedRow } from "@/lib/api";
import { FakeEventSource, installFakeEventSource } from "@/test/fake-event-source";

import { EventsPage } from "./events";

const eventsFeedMock = vi.hoisted(() => vi.fn());
const namespacesMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { eventsFeed: eventsFeedMock, namespaces: { list: namespacesMock } },
  };
});

const feed: EventFeedRow[] = [
  {
    name: "e1",
    namespace: "default",
    type: "Warning",
    reason: "BackOff",
    message: "back-off restarting failed container",
    count: 5,
    lastSeen: "2026-07-15T10:02:00Z",
    involvedObject: { kind: "Pod", namespace: "default", name: "web-1" },
  },
  {
    name: "e2",
    namespace: "default",
    type: "Normal",
    reason: "Pulled",
    message: "image pulled",
    count: 1,
    lastSeen: "2026-07-15T10:01:00Z",
    involvedObject: { kind: "ConfigMap", namespace: "default", name: "cfg" },
  },
];

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/events"]}>
        <EventsPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

let restore: () => void;
beforeEach(() => {
  restore = installFakeEventSource();
  eventsFeedMock.mockReset().mockResolvedValue(feed);
  namespacesMock.mockReset().mockResolvedValue(["default", "kube-system"]);
});
afterEach(() => restore());

describe("EventsPage", () => {
  it("lists events with columns and deep-links the involved object when routable", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByText("BackOff")).toBeInTheDocument());

    // A Pod deep-links to its detail route; a ConfigMap (no typed route) does not.
    const podLink = screen.getByRole("link", { name: "Pod/web-1" });
    expect(podLink).toHaveAttribute("href", "/resources/core/v1/pods/default/web-1");
    expect(screen.queryByRole("link", { name: "ConfigMap/cfg" })).not.toBeInTheDocument();
    expect(screen.getByText("ConfigMap/cfg")).toBeInTheDocument();

    // Count column shows the repeat multiplier.
    expect(screen.getByText("×5")).toBeInTheDocument();
  });

  it("filters by type client-side", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByText("Pulled")).toBeInTheDocument());

    fireEvent.change(screen.getByLabelText("Event type"), { target: { value: "Warning" } });
    expect(screen.getByText("BackOff")).toBeInTheDocument();
    expect(screen.queryByText("Pulled")).not.toBeInTheDocument();
  });

  it("appends new events live from the stream without a refetch", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByText("BackOff")).toBeInTheDocument());
    act(() => FakeEventSource.latest().emitOpen());

    act(() =>
      FakeEventSource.latest().emitMessage(
        JSON.stringify({
          type: "add",
          row: {
            name: "e3",
            namespace: "default",
            type: "Warning",
            reason: "FailedScheduling",
            message: "0/3 nodes are available",
            count: 1,
            lastSeen: "2026-07-15T10:03:00Z",
            involvedObject: { kind: "Pod", namespace: "default", name: "web-2" },
          },
        }),
      ),
    );

    await waitFor(() => expect(screen.getByText("FailedScheduling")).toBeInTheDocument());
    // Newest-first: the live event sorts to the top.
    const rows = within(screen.getByRole("table")).getAllByRole("row");
    expect(within(rows[1]).getByText("FailedScheduling")).toBeInTheDocument();
    expect(eventsFeedMock).toHaveBeenCalledTimes(1);
  });
});
