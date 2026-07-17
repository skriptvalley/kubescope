import { render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { SetupState } from "@/lib/api";
import { connectivity } from "@/lib/connectivity";

import { Layout } from "./layout";

const setupMock = vi.hoisted(() => vi.fn());

// Keep the shell's data-fetching children inert; the gate logic under test lives
// in Layout itself and only depends on the setup state + connectivity store.
vi.mock("@/hooks/use-setup", () => ({ useSetupState: setupMock }));
vi.mock("@/hooks/use-config", () => ({ useServerConfig: () => ({ data: { readOnly: false } }) }));
vi.mock("@/components/sidebar", () => ({ Sidebar: () => null }));
vi.mock("@/components/global-search", () => ({ GlobalSearch: () => null }));
vi.mock("@/components/context-switcher", () => ({ ContextSwitcher: () => null }));
vi.mock("@/components/shortcuts-help", () => ({ ShortcutsHelp: () => null }));
vi.mock("@/components/active-forwards-panel", () => ({ ActiveForwardsPanel: () => null }));
vi.mock("@/components/cluster-banner", () => ({ ClusterBanner: () => null }));
vi.mock("@/pages/starter", () => ({
  StarterPage: ({ state }: { state: SetupState }) => (
    <div data-testid="starter">{state.state}</div>
  ),
}));

function renderLayout() {
  const router = createMemoryRouter(
    [
      {
        path: "/",
        element: <Layout />,
        children: [{ index: true, element: <div data-testid="outlet" /> }],
      },
    ],
    { initialEntries: ["/"] },
  );
  return render(<RouterProvider router={router} />);
}

function setup(state: SetupState["state"] | undefined) {
  const data: SetupState | undefined =
    state === undefined
      ? undefined
      : { state, kubeconfigSources: ["/kubeconfig"], canSetKubeconfig: true };
  setupMock.mockReturnValue({ data });
}

afterEach(() => {
  setupMock.mockReset();
  connectivity.resetForTests();
});

describe("Layout setup gate", () => {
  it.each(["no_kubeconfig", "no_contexts", "no_active_context"] as const)(
    "renders the starter for %s",
    (state) => {
      setup(state);
      renderLayout();
      expect(screen.getByTestId("starter")).toHaveTextContent(state);
      expect(screen.queryByTestId("outlet")).toBeNull();
    },
  );

  it("renders the starter for active_unreachable before any successful connection", () => {
    setup("active_unreachable");
    renderLayout();
    expect(screen.getByTestId("starter")).toBeInTheDocument();
    expect(screen.queryByTestId("outlet")).toBeNull();
  });

  it("renders the outlet (not the starter) for active_unreachable once ever-connected", () => {
    connectivity.markEverConnected();
    setup("active_unreachable");
    renderLayout();
    expect(screen.getByTestId("outlet")).toBeInTheDocument();
    expect(screen.queryByTestId("starter")).toBeNull();
  });

  it("renders the outlet when ready", () => {
    setup("ready");
    renderLayout();
    expect(screen.getByTestId("outlet")).toBeInTheDocument();
    expect(screen.queryByTestId("starter")).toBeNull();
  });

  it("renders the outlet while setup is still loading (nothing cached)", () => {
    setup(undefined);
    renderLayout();
    expect(screen.getByTestId("outlet")).toBeInTheDocument();
    expect(screen.queryByTestId("starter")).toBeNull();
  });
});
