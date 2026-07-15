import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { FakeWebSocket, installFakeWebSocket } from "@/test/fake-web-socket";

import { openExecSocket, type ExecStatus } from "./exec-socket";

let restore: () => void;
beforeEach(() => {
  restore = installFakeWebSocket();
});
afterEach(() => restore());

const decoder = new TextDecoder();

describe("openExecSocket", () => {
  it("opens an absolute ws:// URL from a relative /api path", () => {
    openExecSocket("/api/v1/stream/pods/default/web/exec?container=app", {});
    const ws = FakeWebSocket.latest();
    expect(ws.url).toMatch(/^ws:\/\/[^/]+\/api\/v1\/stream\/pods\/default\/web\/exec\?container=app$/);
    expect(ws.binaryType).toBe("arraybuffer");
  });

  it("reports status transitions connecting → open → closed", () => {
    const statuses: ExecStatus[] = [];
    openExecSocket("/api/v1/stream/pods/default/web/exec", { onStatus: (s) => statuses.push(s) });
    const ws = FakeWebSocket.latest();
    expect(statuses).toEqual(["connecting"]);
    ws.emitOpen();
    ws.emitClose();
    expect(statuses).toEqual(["connecting", "open", "closed"]);
  });

  it("delivers binary stdout frames to onData", () => {
    let received: string | undefined;
    openExecSocket("/api/v1/stream/pods/default/web/exec", {
      onData: (bytes) => {
        received = decoder.decode(bytes);
      },
    });
    const ws = FakeWebSocket.latest();
    ws.emitOpen();
    ws.emitBinary(new TextEncoder().encode("hello\n"));
    expect(received).toBe("hello\n");
  });

  it("surfaces an exit control frame with the code", () => {
    let exit: { reason: string } | undefined;
    openExecSocket("/api/v1/stream/pods/default/web/exec", { onExit: (e) => (exit = e) });
    const ws = FakeWebSocket.latest();
    ws.emitOpen();
    ws.emitControl({ type: "exit", code: 7 });
    expect(exit?.reason).toContain("7");
  });

  it("surfaces an error control frame with the message", () => {
    let exit: { reason: string } | undefined;
    openExecSocket("/api/v1/stream/pods/default/web/exec", { onExit: (e) => (exit = e) });
    const ws = FakeWebSocket.latest();
    ws.emitOpen();
    ws.emitControl({ type: "error", message: "pods \"web\" is forbidden" });
    expect(exit?.reason).toBe('pods "web" is forbidden');
  });

  it("sends stdin as a binary frame (not text)", () => {
    const handle = openExecSocket("/api/v1/stream/pods/default/web/exec", {});
    const ws = FakeWebSocket.latest();
    ws.emitOpen();
    handle.send("ls\n");
    expect(ws.sent).toHaveLength(1);
    const frame = ws.sent[0];
    expect(frame).toBeInstanceOf(Uint8Array);
    expect(decoder.decode(frame as Uint8Array)).toBe("ls\n");
  });

  it("sends a resize as a JSON control frame", () => {
    const handle = openExecSocket("/api/v1/stream/pods/default/web/exec", {});
    const ws = FakeWebSocket.latest();
    ws.emitOpen();
    handle.resize(120, 40);
    expect(ws.sent).toEqual([JSON.stringify({ type: "resize", cols: 120, rows: 40 })]);
  });

  it("does not send before the socket is open", () => {
    const handle = openExecSocket("/api/v1/stream/pods/default/web/exec", {});
    const ws = FakeWebSocket.latest();
    handle.send("early");
    handle.resize(80, 24);
    expect(ws.sent).toHaveLength(0);
  });

  it("close() stops the socket and suppresses further status", () => {
    const statuses: ExecStatus[] = [];
    const handle = openExecSocket("/api/v1/stream/pods/default/web/exec", {
      onStatus: (s) => statuses.push(s),
    });
    const ws = FakeWebSocket.latest();
    ws.emitOpen();
    handle.close();
    expect(ws.readyState).toBe(FakeWebSocket.CLOSED);
    // A late close event after an explicit close must not re-emit "closed".
    ws.emitClose();
    expect(statuses.filter((s) => s === "closed")).toHaveLength(0);
  });
});
