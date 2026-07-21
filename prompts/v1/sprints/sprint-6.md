# Sprint 6 — Exec terminal + port-forward

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching.
3. ADRs: `docs/adr/0006-live-updates-sse-and-streaming-websocket.md` (WebSocket for exec/terminal; why not SSE here).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Operate inside pods from the browser: a working in-browser exec terminal and pod port-forwarding.

## Stories

### Story 6.1 — WebSocket exec bridge (backend: coder/websocket ⇆ SPDY exec)
Bridge in `internal/stream`: browser WebSocket (coder/websocket) on one side, client-go SPDY executor (`pods/exec`, `remotecommand`) on the other. Pipe stdin/stdout/stderr with TTY, handle resize control messages, tear down cleanly when either side closes.
**Acceptance criteria:**
- [ ] Exec endpoint upgrades to WebSocket and attaches to the target pod/container with a selectable command (default `/bin/sh`).
- [ ] stdin/stdout/stderr piped both directions; resize control messages change the remote TTY size.
- [ ] Closing the WebSocket terminates the SPDY session (no leaked goroutines); remote process exit closes the WebSocket with a structured close reason.
- [ ] Errors (pod gone, bad container name, RBAC denied) surface as a structured close message, never a hang.
- [ ] `KUBESCOPE_READ_ONLY=true` rejects exec with 403 before the upgrade.

### Story 6.2 — xterm.js terminal UI (container select, resize, reconnect)
Terminal tab on pod detail using xterm.js 5: container selector, fit-to-panel resizing, explicit reconnect after disconnect.
**Acceptance criteria:**
- [ ] Pod detail has a Terminal tab; opening it yields an interactive shell in the selected container.
- [ ] Container dropdown switches the exec target (fresh session per switch).
- [ ] Panel/browser resize triggers xterm fit + a resize message to the backend.
- [ ] On disconnect, the terminal shows a clear "session ended" state with one-click reconnect.
- [ ] Terminal I/O is never written to server logs.

### Story 6.3 — Port-forward (start/stop, list active forwards)
Backend-managed port-forward sessions (client-go SPDY port-forward) with an API + UI to start, stop, and list active forwards for the current context.
**Acceptance criteria:**
- [ ] Start a forward from pod detail (pod port → backend listen port); traffic flows end-to-end.
- [ ] Active-forwards panel lists pod, ports, context, uptime; stop closes the listener immediately.
- [ ] Forwards are torn down on context switch and on server shutdown.
- [ ] Failures (port in use, pod deleted mid-forward) surface in the UI and the dead forward is removed from the list.
- [ ] `KUBESCOPE_READ_ONLY=true` blocks starting new forwards.

## Task checklist
- [ ] Add exec WebSocket route + upgrade handler in `internal/stream` (coder/websocket).
- [ ] Wire client-go `remotecommand` SPDY executor: stdin/stdout/stderr, TTY, resize queue.
- [ ] Define the WS wire protocol (binary data frames + JSON control frames for resize/close) and document it in `docs/ARCHITECTURE.md`.
- [ ] Enforce read-only middleware on exec and port-forward-start endpoints.
- [ ] Frontend: Terminal tab on pod detail — xterm.js + fit addon, container selector, copy/paste.
- [ ] Frontend: disconnect/"session ended" state + reconnect action.
- [ ] Backend: port-forward manager in `internal/stream` — per-context registry, start/stop/list, idempotent stop.
- [ ] API: create / list / delete port-forward endpoints.
- [ ] Frontend: port-forward controls on pod detail + global active-forwards panel.
- [ ] Cleanup: terminate all exec sessions and forwards on context switch and graceful shutdown.
- [ ] Docs: note that reaching a forwarded port from the host requires publishing it on the container (`-p`), alongside the existing Docker run docs.
- [ ] Cover everything in Testing requirements below.

## Testing requirements
- Unit: WS wire-protocol encode/decode (data vs control frames, resize payloads); port-forward registry lifecycle (start/stop/list, double-stop, teardown on context switch); read-only 403 on exec and forward-start.
- Handler tests: parameter validation (pod/container/command), auth/context resolution errors return structured failures.
- Manual kind smoke: exec into a running pod, run commands, resize the window, delete the pod mid-session (clean close); start a forward to an nginx pod, `curl` through it, stop it, confirm the listener is gone.

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-6/<slug>`, open PR.
4. Agent code review on the PR diff; fix real findings on the branch (or log them as FB-N).
5. When gates are green (`make test` + `make lint` + `make fe-test`; green CI once Sprint 8 lands): squash-merge with a Conventional subject, delete the branch, sync local `main` (`git checkout main && git pull --prune`).
6. Print a concise summary: outcome + blockers only. The session ends with the work merged and the repo clean on up-to-date `main`.
