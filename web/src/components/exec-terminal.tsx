import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { RotateCw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { podExecUrl, type KubeObject } from "@/lib/api";
import { openExecSocket, type ExecSocketHandle, type ExecStatus } from "@/lib/exec-socket";

interface SpecContainer {
  name: string;
}

/** Container names off a Pod object (init containers first), matching the log
 *  viewer's ordering so the two selectors agree. */
function containerNames(object?: KubeObject): string[] {
  const spec = (object?.spec ?? {}) as { containers?: SpecContainer[]; initContainers?: SpecContainer[] };
  return [...(spec.initContainers ?? []), ...(spec.containers ?? [])].map((c) => c.name);
}

/** In-browser exec terminal (Story 6.2): xterm.js bridged to the exec WebSocket,
 *  with a container selector (fresh session per switch), fit-to-panel resizing,
 *  and an explicit reconnect after the session ends. Terminal I/O never touches
 *  the server logs (backend) nor any client store — it lives only in xterm. */
export function ExecTerminal({
  namespace,
  name,
  object,
}: {
  namespace: string;
  name: string;
  object?: KubeObject;
}) {
  const containers = containerNames(object);
  const containersKey = containers.join(" ");

  const [container, setContainer] = useState("");
  // Bumped to force a fresh session (reconnect) without changing the container.
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<ExecStatus>("connecting");
  const [ended, setEnded] = useState<{ reason: string } | null>(null);

  const hostRef = useRef<HTMLDivElement>(null);

  // Default to the first container once the pod object loads.
  useEffect(() => {
    if (container === "" && containers.length > 0) setContainer(containers[0]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containersKey]);

  const url = useMemo(
    () => podExecUrl(namespace, name, { container: container || undefined }),
    [namespace, name, container],
  );

  // One session per (url, attempt): switching container or reconnecting builds a
  // brand-new xterm + socket and tears the previous pair down.
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    // Wait for the default container to be picked before connecting, so a pod
    // with containers opens exactly one session (not a throwaway "" one first).
    if (containers.length > 0 && container === "") return;
    setEnded(null);
    setStatus("connecting");

    const term = new Terminal({
      cursorBlink: true,
      convertEol: true,
      fontSize: 13,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
      theme: { background: "#09090b", foreground: "#e4e4e7" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    safeFit(fit);

    let socket: ExecSocketHandle | null = null;
    const syncSize = () => {
      safeFit(fit);
      socket?.resize(term.cols, term.rows);
    };

    socket = openExecSocket(url, {
      onStatus: (next) => {
        setStatus(next);
        // Match the remote TTY to the panel once connected (a resize sent while
        // still connecting would be dropped).
        if (next === "open") syncSize();
      },
      onData: (bytes) => term.write(bytes),
      onExit: (exit) => setEnded(exit),
    });

    const dataSub = term.onData((data) => socket?.send(data));

    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== "undefined") {
      observer = new ResizeObserver(() => syncSize());
      observer.observe(host);
    }
    term.focus();

    return () => {
      observer?.disconnect();
      dataSub.dispose();
      socket?.close();
      term.dispose();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, attempt]);

  return (
    <section className="space-y-3" aria-label="Pod terminal">
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-1.5 text-sm">
          <span className="text-muted-foreground">Container</span>
          <select
            className="rounded-md border bg-background px-2 py-1 text-sm"
            value={container}
            onChange={(e) => setContainer(e.target.value)}
            aria-label="Container"
          >
            {containers.length === 0 && <option value="">default</option>}
            {containers.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </label>

        <div className="ml-auto text-xs font-medium text-muted-foreground" role="status">
          {ended ? `Session ended (${ended.reason})` : status === "open" ? "Connected" : "Connecting…"}
        </div>
      </div>

      <div className="relative">
        <div
          ref={hostRef}
          data-testid="terminal-host"
          className="h-96 overflow-hidden rounded-md border bg-zinc-950 p-2"
        />
        {ended && (
          <div
            className="absolute inset-0 flex flex-col items-center justify-center gap-3 rounded-md bg-zinc-950/85 text-sm text-zinc-100"
            role="status"
          >
            <p>Session ended ({ended.reason})</p>
            <Button size="sm" variant="secondary" onClick={() => setAttempt((a) => a + 1)}>
              <RotateCw className="h-4 w-4" />
              Reconnect
            </Button>
          </div>
        )}
      </div>
    </section>
  );
}

/** xterm's FitAddon throws when the host has no measurable size (e.g. jsdom, or
 *  an unmounted panel); a failed fit just leaves the default 80×24. */
function safeFit(fit: FitAddon) {
  try {
    fit.fit();
  } catch {
    // no measurable dimensions yet — keep the default size
  }
}
