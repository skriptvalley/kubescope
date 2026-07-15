import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { KubeObject } from "@/lib/api";
import { FakeEventSource, installFakeEventSource } from "@/test/fake-event-source";

import { LogViewer } from "./log-viewer";

const pod = {
  spec: {
    initContainers: [{ name: "init" }],
    containers: [{ name: "app" }, { name: "sidecar" }],
  },
} as unknown as KubeObject;

let restore: () => void;
beforeEach(() => {
  restore = installFakeEventSource();
});
afterEach(() => restore());

function emitLine(text: string) {
  act(() => FakeEventSource.latest().emitMessage(JSON.stringify({ line: text })));
}

function defineScroll(el: HTMLElement, scrollHeight: number, clientHeight: number, scrollTop: number) {
  Object.defineProperty(el, "scrollHeight", { value: scrollHeight, configurable: true });
  Object.defineProperty(el, "clientHeight", { value: clientHeight, configurable: true });
  el.scrollTop = scrollTop;
}

describe("LogViewer", () => {
  it("renders container options from the pod, defaulting to the first", async () => {
    render(<LogViewer namespace="default" name="web-1" object={pod} />);
    const select = screen.getByLabelText("Container") as HTMLSelectElement;
    const options = within(select).getAllByRole("option").map((o) => o.textContent);
    expect(options).toEqual(["init", "app", "sidecar"]);
    await waitFor(() => expect(select.value).toBe("init"));
  });

  it("maps controls into the stream URL (container, previous forces follow off, tail)", async () => {
    render(<LogViewer namespace="default" name="web-1" object={pod} />);
    await waitFor(() => expect((screen.getByLabelText("Container") as HTMLSelectElement).value).toBe("init"));

    fireEvent.change(screen.getByLabelText("Container"), { target: { value: "sidecar" } });
    fireEvent.click(screen.getByLabelText("Previous"));
    fireEvent.change(screen.getByLabelText("Tail lines"), { target: { value: "50" } });

    const url = FakeEventSource.latest().url;
    expect(url).toContain("container=sidecar");
    expect(url).toContain("previous=true");
    expect(url).toContain("tailLines=50");
    // Previous disables follow in the UI.
    expect((screen.getByLabelText("Follow") as HTMLInputElement).disabled).toBe(true);
  });

  it("appends streamed lines and surfaces stream close", async () => {
    render(<LogViewer namespace="default" name="web-1" object={pod} />);
    await waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));

    emitLine("line one");
    emitLine("line two");
    const output = screen.getByTestId("log-output");
    await waitFor(() => {
      expect(within(output).getByText("line one")).toBeInTheDocument();
      expect(within(output).getByText("line two")).toBeInTheDocument();
    });

    act(() => FakeEventSource.latest().emitNamed("closed", JSON.stringify({ reason: "eof" })));
    await waitFor(() => expect(screen.getByText(/stream closed \(eof\)/i)).toBeInTheDocument());
  });

  it("pauses auto-scroll when scrolled up and resumes via Jump to latest", async () => {
    render(<LogViewer namespace="default" name="web-1" object={pod} />);
    const output = screen.getByTestId("log-output");
    defineScroll(output, 1000, 100, 0);

    // Pinned to bottom: a new line auto-scrolls to the bottom.
    emitLine("first");
    await waitFor(() => expect(output.scrollTop).toBe(1000));

    // Scroll up → auto-scroll pauses, "Jump to latest" appears.
    defineScroll(output, 1000, 100, 0);
    fireEvent.scroll(output);
    await waitFor(() => expect(screen.getByRole("button", { name: /jump to latest/i })).toBeInTheDocument());

    // While paused, new lines do not move the viewport.
    emitLine("second");
    expect(output.scrollTop).toBe(0);

    // Jump to latest re-pins and hides the button.
    fireEvent.click(screen.getByRole("button", { name: /jump to latest/i }));
    expect(output.scrollTop).toBe(1000);
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: /jump to latest/i })).not.toBeInTheDocument(),
    );
  });
});
