import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ContextInfo } from "@/lib/api";
import { connectivity } from "@/lib/connectivity";
import { healthBadge, healthTone } from "@/lib/context-health";

import { ContextSwitcher } from "./context-switcher";

const listMock = vi.hoisted(() => vi.fn());
const healthMock = vi.hoisted(() => vi.fn());
const switchMock = vi.hoisted(() => vi.fn());
const setupMock = vi.hoisted(() => vi.fn());
const kubeconfigsListMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: {
      contexts: { list: listMock, health: healthMock, switch: switchMock },
      setup: { state: setupMock },
      kubeconfigs: { list: kubeconfigsListMock },
    },
  };
});

// Default: a ready server with the source registry locked. Individual cases
// override the setup state (FB-9 muted branch) or canSetKubeconfig (manage entry).
beforeEach(() => {
  setupMock.mockReset().mockResolvedValue({
    state: "ready",
    kubeconfigSources: ["/kubeconfig"],
    canSetKubeconfig: false,
  });
  kubeconfigsListMock.mockReset().mockResolvedValue({ sources: [], canSetKubeconfig: true });
});
afterEach(() => connectivity.resetForTests());

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

describe("healthTone", () => {
  it("maps a healthy cluster to the brand (ok) tone", () => {
    const t = healthTone({ name: "x", reachable: true, authOK: true, serverVersion: "v1.33.0" }, false);
    expect(t.tone).toBe("ok");
    expect(t.label).toBe("Healthy");
  });
  it("maps an unreachable cluster to the destructive (warn) tone — never ok", () => {
    const t = healthTone({ name: "x", reachable: false, authOK: false, serverVersion: "", error: "refused" }, false);
    expect(t.tone).toBe("warn");
    expect(t.label).toBe("Unreachable");
  });
  it("maps an auth failure to the destructive (warn) tone", () => {
    const t = healthTone({ name: "x", reachable: true, authOK: false, serverVersion: "" }, false);
    expect(t.tone).toBe("warn");
  });
  it("maps a pending probe to the neutral tone", () => {
    expect(healthTone(undefined, true).tone).toBe("neutral");
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

    // 'dev' only appears in the open menu (the trigger shows the active 'prod').
    expect(await screen.findByText("dev")).toBeInTheDocument();
    expect(screen.getByText("Healthy")).toBeInTheDocument();
    expect(screen.getByText("Unreachable")).toBeInTheDocument();
  });

  it("switches context, globally invalidates mounted views, and drops only inactive caches", async () => {
    listMock.mockResolvedValue(contexts);
    healthMock.mockResolvedValue([]);
    switchMock.mockResolvedValue([
      { name: "prod", cluster: "c-prod", namespace: "default", active: false },
      { name: "dev", cluster: "c-dev", namespace: "default", active: true },
    ]);
    const { queryClient } = renderSwitcher();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const removeSpy = vi.spyOn(queryClient, "removeQueries");

    const trigger = await screen.findByRole("button", { name: /switch context/i });
    await waitFor(() => expect(trigger).toBeEnabled());
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByText("dev"));

    // TanStack Query v5 passes a second context arg to the mutation fn.
    await waitFor(() => expect(switchMock).toHaveBeenCalledWith("dev", expect.anything()));
    // Global invalidation (no filter args) refetches every *mounted* view in place.
    await waitFor(() => expect(invalidateSpy).toHaveBeenCalledWith());
    // Only *inactive* cluster caches are dropped — removing an active query would
    // strand the current page on stale data (no observer left to refetch it).
    expect(removeSpy).toHaveBeenCalledWith({ queryKey: ["overview"], type: "inactive" });
    expect(removeSpy).toHaveBeenCalledWith({ queryKey: ["nodes"], type: "inactive" });
    expect(removeSpy).not.toHaveBeenCalledWith({ queryKey: ["overview"] });
  });

  it("surfaces a failed switch instead of silently reverting", async () => {
    listMock.mockResolvedValue(contexts);
    healthMock.mockResolvedValue([]);
    switchMock.mockRejectedValue(new ApiError('unknown context "dev"', "unknown_context", 404));
    renderSwitcher();

    const trigger = await screen.findByRole("button", { name: /switch context/i });
    await waitFor(() => expect(trigger).toBeEnabled());
    fireEvent.click(trigger);
    fireEvent.click(await screen.findByText("dev"));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/switch failed/i);
    expect(alert).toHaveTextContent(/unknown_context/);
  });

  it("shows a neutral muted label (not a red error) when contexts fail before the server is ready", async () => {
    listMock.mockRejectedValue(new ApiError("cannot load kubeconfig", "kubeconfig_unavailable", 503));
    healthMock.mockResolvedValue([]);
    setupMock.mockResolvedValue({ state: "no_kubeconfig", kubeconfigSources: [], canSetKubeconfig: true });
    renderSwitcher();

    expect(await screen.findByText("no cluster")).toBeInTheDocument();
    expect(screen.queryByText("kubeconfig error")).toBeNull();
  });

  it("shows the red kubeconfig error when contexts fail while the server is ready", async () => {
    listMock.mockRejectedValue(new ApiError("cannot load kubeconfig", "kubeconfig_unavailable", 503));
    healthMock.mockResolvedValue([]);
    setupMock.mockResolvedValue({ state: "ready", kubeconfigSources: ["/kubeconfig"], canSetKubeconfig: false });
    renderSwitcher();

    expect(await screen.findByText("kubeconfig error")).toBeInTheDocument();
    expect(screen.queryByText("no cluster")).toBeNull();
  });

  it("offers a Manage kubeconfig sources entry when the registry is editable", async () => {
    listMock.mockResolvedValue(contexts);
    healthMock.mockResolvedValue([]);
    setupMock.mockResolvedValue({ state: "ready", kubeconfigSources: ["/kubeconfig"], canSetKubeconfig: true });
    renderSwitcher();

    const trigger = await screen.findByRole("button", { name: /switch context/i });
    await waitFor(() => expect(trigger).toBeEnabled());
    fireEvent.click(trigger);

    expect(await screen.findByText("Manage kubeconfig sources")).toBeInTheDocument();
  });

  it("hides the Manage kubeconfig sources entry when the registry is locked", async () => {
    listMock.mockResolvedValue(contexts);
    healthMock.mockResolvedValue([]);
    setupMock.mockResolvedValue({ state: "ready", kubeconfigSources: ["/kubeconfig"], canSetKubeconfig: false });
    renderSwitcher();

    const trigger = await screen.findByRole("button", { name: /switch context/i });
    await waitFor(() => expect(trigger).toBeEnabled());
    fireEvent.click(trigger);

    expect(await screen.findByText("dev")).toBeInTheDocument();
    expect(screen.queryByText("Manage kubeconfig sources")).toBeNull();
  });
});
