import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ContextInfo } from "@/lib/api";
import { healthBadge } from "@/lib/context-health";

import { ContextSwitcher } from "./context-switcher";

const listMock = vi.hoisted(() => vi.fn());
const healthMock = vi.hoisted(() => vi.fn());
const switchMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: { contexts: { list: listMock, health: healthMock, switch: switchMock } },
  };
});

const contexts: ContextInfo[] = [
  { name: "prod", cluster: "c-prod", namespace: "default", active: true },
  { name: "dev", cluster: "c-dev", namespace: "default", active: false },
];

function renderSwitcher() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <ContextSwitcher />
    </QueryClientProvider>,
  );
  return { queryClient, ...utils };
}

describe("healthBadge", () => {
  it("shows Checking while the probe is pending", () => {
    expect(healthBadge(undefined, true).label).toBe("Checking");
  });
  it("shows Healthy when reachable and authenticated", () => {
    const badge = healthBadge(
      { name: "x", reachable: true, authOK: true, serverVersion: "v1.33.0" },
      false,
    );
    expect(badge.label).toBe("Healthy");
    expect(badge.variant).toBe("default");
  });
  it("shows Auth error when reachable but not authenticated", () => {
    const badge = healthBadge(
      { name: "x", reachable: true, authOK: false, serverVersion: "", guidance: "mount ~/.aws" },
      false,
    );
    expect(badge.label).toBe("Auth error");
    expect(badge.title).toBe("mount ~/.aws");
  });
  it("shows Unreachable when the cluster cannot be reached", () => {
    const badge = healthBadge(
      { name: "x", reachable: false, authOK: false, serverVersion: "", error: "refused" },
      false,
    );
    expect(badge.label).toBe("Unreachable");
  });
});

describe("ContextSwitcher", () => {
  it("shows the active context on the trigger", async () => {
    listMock.mockResolvedValue(contexts);
    healthMock.mockResolvedValue([]);
    renderSwitcher();
    expect(await screen.findByText("prod")).toBeInTheDocument();
  });

  it("opens the menu and renders a status badge per context", async () => {
    listMock.mockResolvedValue(contexts);
    healthMock.mockResolvedValue([
      { name: "prod", reachable: true, authOK: true, serverVersion: "v1.33.0" },
      { name: "dev", reachable: false, authOK: false, serverVersion: "", error: "refused" },
    ]);
    renderSwitcher();

    const trigger = await screen.findByRole("button", { name: /switch context/i });
    await waitFor(() => expect(trigger).toBeEnabled());
    fireEvent.click(trigger);

    expect(await screen.findByRole("listbox")).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    expect(screen.getByText("Unreachable")).toBeInTheDocument();
  });

  it("switches context and invalidates all queries so views refetch", async () => {
    listMock.mockResolvedValue(contexts);
    healthMock.mockResolvedValue([]);
    switchMock.mockResolvedValue([
      { name: "prod", cluster: "c-prod", namespace: "default", active: false },
      { name: "dev", cluster: "c-dev", namespace: "default", active: true },
    ]);
    const { queryClient } = renderSwitcher();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    const trigger = await screen.findByRole("button", { name: /switch context/i });
    await waitFor(() => expect(trigger).toBeEnabled());
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByText("dev"));

    // TanStack Query v5 passes a second context arg to the mutation fn.
    await waitFor(() => expect(switchMock).toHaveBeenCalledWith("dev", expect.anything()));
    await waitFor(() => expect(invalidateSpy).toHaveBeenCalled());
  });
});
