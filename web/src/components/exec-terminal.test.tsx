import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { KubeObject } from "@/lib/api";
import { FakeWebSocket, installFakeWebSocket } from "@/test/fake-web-socket";

// xterm.js needs a real canvas/layout that jsdom lacks, so stub it: the terminal
// behaviour under test is the container selector, session lifecycle and reconnect
// wiring, not xterm's rendering.
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    loadAddon() {}
    open() {}
    write() {}
    focus() {}
    dispose() {}
    onData() {
      return { dispose() {} };
    }
  },
}));
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit() {}
    activate() {}
    dispose() {}
  },
}));

import { ExecTerminal } from "./exec-terminal";

const pod = {
  kind: "Pod",
  spec: { initContainers: [{ name: "init" }], containers: [{ name: "app" }, { name: "sidecar" }] },
} as unknown as KubeObject;

let restore: () => void;
beforeEach(() => {
  restore = installFakeWebSocket();
});
afterEach(() => restore());

function lastUrl(): string {
  return FakeWebSocket.latest().url;
}

describe("ExecTerminal", () => {
  it("opens an exec session for the first container", async () => {
    render(<ExecTerminal namespace="default" name="web-1" object={pod} />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    expect(lastUrl()).toContain("/api/v1/stream/pods/default/web-1/exec");
    expect(lastUrl()).toContain("container=init");
  });

  it("opens a fresh session when the container changes", async () => {
    render(<ExecTerminal namespace="default" name="web-1" object={pod} />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));

    act(() => {
      const select = screen.getByLabelText("Container") as HTMLSelectElement;
      select.value = "sidecar";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });

    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(2));
    expect(lastUrl()).toContain("container=sidecar");
  });

  it("shows a session-ended state with reconnect, and reconnect opens a new session", async () => {
    render(<ExecTerminal namespace="default" name="web-1" object={pod} />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));

    act(() => {
      const ws = FakeWebSocket.latest();
      ws.emitOpen();
      ws.emitControl({ type: "exit", code: 0 });
    });

    const reconnect = await screen.findByRole("button", { name: /reconnect/i });
    expect(screen.getAllByText(/session ended/i).length).toBeGreaterThan(0);

    act(() => reconnect.click());

    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(2));
  });
});
