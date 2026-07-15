import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { EventSummary } from "@/lib/api";

import { EventsPanel } from "./events-panel";

const eventsMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: { events: eventsMock } };
});

function renderPanel() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <EventsPanel kind="Pod" namespace="default" name="web-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => eventsMock.mockReset());

describe("EventsPanel", () => {
  it("renders events newest-first in the order returned, with counts and warning distinction", async () => {
    // The backend returns events pre-sorted newest-first; the panel preserves it.
    const events: EventSummary[] = [
      { type: "Warning", reason: "BackOff", message: "back-off restarting", count: 5, lastSeen: "2026-07-15T10:02:00Z" },
      { type: "Normal", reason: "Pulled", message: "image pulled", count: 1, lastSeen: "2026-07-15T10:01:00Z" },
      { type: "Normal", reason: "Scheduled", message: "assigned", count: 1, lastSeen: "2026-07-15T10:00:00Z" },
    ];
    eventsMock.mockResolvedValue(events);

    renderPanel();

    const list = await screen.findByTestId("events-list");
    const rows = within(list).getAllByRole("listitem");
    expect(rows.map((r) => within(r).getByText(/BackOff|Pulled|Scheduled/).textContent)).toEqual([
      "BackOff",
      "Pulled",
      "Scheduled",
    ]);
    // Repeated event shows its count; a warning renders its Warning label.
    expect(within(rows[0]).getByText("×5")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Warning")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Normal")).toBeInTheDocument();
  });

  it("shows a clean empty state when there are no events", async () => {
    eventsMock.mockResolvedValue([]);
    renderPanel();
    await waitFor(() =>
      expect(screen.getByText(/no events recorded for this object/i)).toBeInTheDocument(),
    );
  });
});
