import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError, type ContextInfo, type SetupState } from "@/lib/api";
import { connectivity } from "@/lib/connectivity";

import { StarterPage } from "./starter";

const listMock = vi.hoisted(() => vi.fn());
const healthMock = vi.hoisted(() => vi.fn());
const switchMock = vi.hoisted(() => vi.fn());
const setKubeconfigMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...original,
    api: {
      contexts: { list: listMock, health: healthMock, switch: switchMock },
      setup: { setKubeconfig: setKubeconfigMock },
    },
  };
});

function makeState(overrides: Partial<SetupState> & Pick<SetupState, "state">): SetupState {
  return { kubeconfigPath: "/kubeconfig", canSetKubeconfig: true, ...overrides };
}

function renderStarter(state: SetupState) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <StarterPage state={state} />
    </QueryClientProvider>,
  );
  return { queryClient, ...utils };
}

beforeEach(() => {
  listMock.mockReset().mockResolvedValue([]);
  healthMock.mockReset().mockResolvedValue([]);
  switchMock.mockReset().mockResolvedValue([]);
  setKubeconfigMock.mockReset();
});
afterEach(() => connectivity.resetForTests());

describe("StarterPage", () => {
  it("no_kubeconfig: explains kubeconfig setup with mount instructions and a doc link", () => {
    renderStarter(
      makeState({ state: "no_kubeconfig", docURL: "https://docs.example/adr-0004" }),
    );
    expect(screen.getByText(/no kubeconfig found/i)).toBeInTheDocument();
    expect(screen.getByText("KUBESCOPE_KUBECONFIG")).toBeInTheDocument();
    expect(screen.getByText(/docker run -v/)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /docker\/auth guide/i });
    expect(link).toHaveAttribute("href", "https://docs.example/adr-0004");
  });

  it("no_contexts: shows the path and how to add a context", () => {
    renderStarter(makeState({ state: "no_contexts" }));
    expect(screen.getByText(/kubeconfig has no contexts/i)).toBeInTheDocument();
    expect(screen.getByText(/kind export kubeconfig/)).toBeInTheDocument();
  });

  it("no_active_context: lists the contexts to pick from", async () => {
    const contexts: ContextInfo[] = [
      { name: "prod", cluster: "c", namespace: "default", active: false },
      { name: "dev", cluster: "c", namespace: "default", active: false },
    ];
    listMock.mockResolvedValue(contexts);
    renderStarter(makeState({ state: "no_active_context" }));
    expect(screen.getByText("Pick a context")).toBeInTheDocument();
    expect(await screen.findByText("prod")).toBeInTheDocument();
    expect(screen.getByText("dev")).toBeInTheDocument();
  });

  it("active_unreachable: shows reason, message, guidance, doc link and Retry", () => {
    renderStarter(
      makeState({
        state: "active_unreachable",
        activeContext: "prod",
        reason: "connection_refused",
        message: "dial tcp 127.0.0.1:6443: connection refused",
        guidance: "the local cluster may be stopped",
        docURL: "https://docs.example/adr-0004",
      }),
    );
    expect(screen.getByText(/cannot reach the cluster/i)).toBeInTheDocument();
    expect(screen.getByText(/connection_refused/)).toBeInTheDocument();
    expect(screen.getByText(/connection refused/i)).toBeInTheDocument();
    expect(screen.getByText(/local cluster may be stopped/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /learn more/i })).toHaveAttribute(
      "href",
      "https://docs.example/adr-0004",
    );
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("set-kubeconfig: a successful submit invalidates the setup query", async () => {
    setKubeconfigMock.mockResolvedValue(makeState({ state: "ready" }));
    const { queryClient } = renderStarter(makeState({ state: "no_kubeconfig" }));
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    fireEvent.change(screen.getByLabelText(/absolute kubeconfig path/i), {
      target: { value: "/other/kubeconfig" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^use$/i }));

    await waitFor(() => expect(setKubeconfigMock).toHaveBeenCalledWith("/other/kubeconfig"));
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["setup"] }),
    );
  });

  it("set-kubeconfig: a failed submit surfaces the error message and guidance", async () => {
    setKubeconfigMock.mockRejectedValue(
      new ApiError("path must be absolute", "invalid_request", 400, "use an absolute path"),
    );
    renderStarter(makeState({ state: "no_kubeconfig" }));

    fireEvent.change(screen.getByLabelText(/absolute kubeconfig path/i), {
      target: { value: "relative" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^use$/i }));

    expect(await screen.findByText(/path must be absolute/i)).toBeInTheDocument();
    expect(screen.getByText(/use an absolute path/i)).toBeInTheDocument();
  });

  it("hides the set-kubeconfig control when canSetKubeconfig is false", () => {
    renderStarter(makeState({ state: "no_kubeconfig", canSetKubeconfig: false }));
    expect(screen.queryByTestId("set-kubeconfig-form")).toBeNull();
    expect(screen.getByTestId("set-kubeconfig-disabled")).toBeInTheDocument();
  });
});

describe("ContextChooser error feedback", () => {
  it("surfaces a failed context switch instead of swallowing it", async () => {
    const contexts: ContextInfo[] = [
      { name: "prod", cluster: "c", namespace: "default", active: false },
    ];
    listMock.mockResolvedValue(contexts);
    switchMock.mockRejectedValue(new ApiError("cannot switch context", "kubeconfig_unavailable", 503));
    renderStarter(makeState({ state: "no_active_context" }));

    fireEvent.click(await screen.findByText("prod"));

    expect(await screen.findByRole("alert")).toHaveTextContent(/switch failed/i);
    expect(screen.getByRole("alert")).toHaveTextContent(/cannot switch context/i);
  });
});
