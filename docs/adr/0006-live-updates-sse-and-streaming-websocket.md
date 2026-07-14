# 0006. Live updates via SSE, streaming via WebSocket

- **Status:** Accepted
- **Date:** 2026-07-14

## Context

The dashboard must feel live: resource lists update on cluster changes and pod logs stream in real time (Sprint 4), and users exec into pods with an interactive terminal (Sprint 6). These are two different transport problems — one-way fan-out vs. bidirectional byte streams — and one transport for both would compromise each.

## Decision

| Concern | Transport | Why |
|---|---|---|
| Resource watch events (watch→SSE bridge, informer-backed) | **SSE** | Unidirectional server→client fits exactly; built-in auto-reconnect with `Last-Event-ID`; plain HTTP so it plays well with proxies and HTTP/2 multiplexing; trivial client (`EventSource`) |
| Pod log streaming (follow mode) | **SSE** | Same one-way shape; reconnect semantics free |
| Exec / terminal | **WebSocket** (coder/websocket) | Genuinely bidirectional: stdin keystrokes up, stdout/stderr down, plus terminal **resize** control messages — SSE cannot carry the upstream leg |
| Port-forward control (Sprint 6) | Plain HTTP API | Start/stop/list are request/response, not streams |

Backend: `internal/stream/` bridges informer events and log streams into per-context SSE fan-out, and bridges the exec WebSocket to the Kubernetes SPDY exec API ([../ARCHITECTURE.md](../ARCHITECTURE.md), [0003](0003-generic-resource-access-via-discovery-and-dynamic-client.md)).
Frontend: SSE events feed TanStack Query cache updates (Story 4.2); xterm.js speaks the exec WebSocket (Story 6.2).

## Consequences

**Positive:**
- Each transport is the simplest correct tool for its job; no custom framing on WebSocket for cases that are really one-way.
- SSE reconnects are handled by the browser; the watch bridge only needs resume/replay logic.
- WebSocket surface is minimized to exec — the one endpoint that truly needs it — limiting proxy/upgrade edge cases.

**Negative:**
- Two streaming stacks to build and test instead of one.
- SSE's per-connection limit over HTTP/1.1 (≈6 per origin) matters if a page opens many streams — mitigated by HTTP/2 and by multiplexing watch events for one context over a single SSE connection.
- Some corporate proxies buffer SSE; requires `X-Accel-Buffering: no` / flush-per-event care.

## Alternatives considered

- **WebSocket for everything** — rejected. Loses free auto-reconnect and HTTP-native behavior for the 90% one-way case; every consumer would need custom reconnect/backoff and message framing that SSE gives for free.
- **Polling only** — rejected as the primary mechanism: not "live", wasteful against the API server at short intervals. **TanStack Query polling remains the fallback** — it ships before the SSE bridge exists and stays as the degradation path when a stream cannot connect.
