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

A port-forward's target may be a pod or a Service — see the FB-13 addendum below.

Backend: `internal/stream/` bridges informer events and log streams into per-context SSE fan-out, and bridges the exec WebSocket to the Kubernetes SPDY exec API ([../ARCHITECTURE.md](../ARCHITECTURE.md), [0003](0003-generic-resource-access-via-discovery-and-dynamic-client.md)).
Frontend: SSE events feed TanStack Query cache updates (Story 4.2); xterm.js speaks the exec WebSocket (Story 6.2).

## FB-13 addendum — service-level port-forward with load balancing

- **Date:** 2026-07-26 (v2 Sprint 1)

The Sprint 6 forward is 1:1 — one SPDY tunnel from one loopback listener to one pod. That is the wrong shape for the common case of "reach this Service locally": it pins every request to one replica, so a caller never sees the Service's real behavior and a single pod restart kills the session. This addendum extends the forward model to **1:N** without changing the transport decision above: port-forward control stays a plain HTTP API, and each leg is still the same client-go SPDY forward.

### Target discrimination

`POST /api/v1/portforwards` now takes a discriminated target, validated as mutually exclusive before any cluster call:

| Target | Body | Meaning |
|---|---|---|
| Pod (Sprint 6, unchanged) | `{namespace, pod, remotePort, localPort?}` | One tunnel to one pod |
| Service (new) | `{namespace, service, servicePort, localPort?}` | One listener load-balancing across the Service's ready endpoints |

The `PortForward` view gains `targetKind` (`pod`\|`service`), `service`, and `backends` — the **live** backend count, recomputed on every `List` so the UI shows the rotation shrinking as pods go away. `GET`/`DELETE` and the read-only guard on create are unchanged.

### Per-connection, not per-request

The balancer hands each **new TCP connection** to the next live backend and splices it bidirectionally; it never parses or buffers the bytes flowing through. That is deliberately the granularity **ClusterIP/kube-proxy** gives: a caller port-forwarding to a Service sees the same distribution it would see in-cluster, including connection reuse pinning an HTTP keep-alive session to one pod. Per-request (L7) balancing was rejected — it would require an HTTP proxy in the data path, make Kubescope's forward behave *unlike* the Service it is standing in for, break every non-HTTP protocol, and put request bytes somewhere they could be inspected or logged. The `io.Discard` posture of the pod forward is preserved end to end: no forwarded bytes and no per-connection metadata are logged; session logs carry only the object names the user asked for.

### Backends are snapshotted at start

Ready endpoints are resolved **once**, when the forward starts, by the same resolver the Service detail view uses (`internal/resources`, reading the Endpoints object — which also gives the per-pod resolution of a named `targetPort` for free). A backend that dies drops out of the rotation and the session survives while ≥1 backend is live; when the last one goes, the session closes exactly as a pod forward does when its pod is deleted. But pods that become ready **after** the forward starts are not picked up.

**Deferred (follow-up):** live EndpointSlice churn-tracking — watching the Service's EndpointSlices and adding/removing per-pod legs as endpoints come and go. It needs an informer per forwarded Service and a rebalancing story for in-flight connections; the snapshot covers the dogfood case (reach a Service locally, hit several replicas) at a fraction of the complexity. Restarting the forward is the workaround until then.

**Death detection is lazy, and that is inherited, not new.** A client-go SPDY forward to a deleted pod does not fail until traffic is pushed through it — the local listener stays bound and the stream only errors on use. So a backend whose pod is gone is discovered by the *first connection routed to it*, which fails; that failure is what drops it from the rotation. Measured against the kind fixture: deleting one of three backends cost exactly one failed connection, after which the survivors carried everything. This is the same window a Sprint 6 pod forward has (it too stays listed until a connection proves it dead) and the same window kube-proxy has between a pod dying and endpoint propagation. It is not closed by buffering and replaying the request, which would require putting request bytes somewhere inspectable — the thing the per-connection design exists to avoid.

### Bounded fan-out

One backend costs a SPDY tunnel plus a loopback listener, so an unbounded rotation would let a single API call open hundreds of apiserver connections. A service forward is capped at **64 ready endpoints**; exceeding it is a classified `422 too_many_backends` failure rather than a silent truncation to a partial rotation — a forward that quietly balanced over 64 of 500 endpoints while claiming to front the Service would be worse than a clear refusal.

### No new dependency

Everything is stdlib `net` (listener, dial, `io.Copy` splice, `CloseWrite` half-close) over the existing `k8s.io/client-go/tools/portforward` + `transport/spdy` legs. No proxy library, no new module.

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
