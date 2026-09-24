// The three URL call sites (fetch, SSE, exec WebSocket) must route through
// withBase (ADR-0012). basePath is read once at import and is "" in tests, so the
// module is mocked here to a real mount to prove every call site honours it.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { FakeEventSource, installFakeEventSource } from "@/test/fake-event-source";
import { FakeWebSocket, installFakeWebSocket } from "@/test/fake-web-socket";

vi.mock("@/lib/base", () => ({
  basePath: "/kubescope",
  withBase: (path: string) => (path.startsWith("/") && !path.startsWith("//") ? `/kubescope${path}` : path),
  routerBasename: () => "/kubescope",
}));

const { api } = await import("./api");
const { openStream } = await import("./stream");
const { openExecSocket } = await import("./exec-socket");

describe("URL call sites under a sub-path mount", () => {
  let restoreES: () => void;
  let restoreWS: () => void;
  beforeEach(() => {
    restoreES = installFakeEventSource();
    restoreWS = installFakeWebSocket();
  });
  afterEach(() => {
    restoreES();
    restoreWS();
    vi.unstubAllGlobals();
  });

  it("fetch goes to <mount>/api", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ readOnly: false, authMode: "none" })));
    vi.stubGlobal("fetch", fetchMock);
    await api.config();
    expect(fetchMock).toHaveBeenCalledWith("/kubescope/api/v1/config", expect.anything());
  });

  it("SSE streams open under the mount", () => {
    const close = openStream("/api/v1/stream/resources/core/v1/pods", {});
    expect(FakeEventSource.latest().url).toBe("/kubescope/api/v1/stream/resources/core/v1/pods");
    close();
  });

  it("the exec WebSocket connects under the mount on the same host", () => {
    const h = openExecSocket("/api/v1/stream/pods/default/web/exec", {});
    expect(FakeWebSocket.latest().url).toBe(`ws://${location.host}/kubescope/api/v1/stream/pods/default/web/exec`);
    h.close();
  });
});
