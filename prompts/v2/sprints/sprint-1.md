# v2 Sprint 1 — Service-level port-forward with load balancing

> v2 sprint (implements **FB-13**). Branch `sprint-v2-1/service-port-forward`.
> One sprint per session. Do not pull FB-14 (graph) work forward. Rules in `CLAUDE.md` apply.

## Context recap
Read before starting (in order):
1. `STATUS.md` — FB-13; the v2 sequence (FB-12 e2e harness is the dogfood enabler — its testenv `frontend` Service, 3× nginx, is the load-balancing fixture).
2. `docs/ARCHITECTURE.md` — the `stream` component; `docs/adr/0006` (port-forward as backend-managed sessions — **this sprint writes an addendum**); `0005` (loopback-only bind, never a LAN interface).
3. `internal/stream/portforward.go` — the existing **pod-level** forward. Extend it; do NOT fork a second manager. Reuse: `PortForwardManager` (start/Stop/List/`CloseOthers`/`CloseAll`, group teardown on context switch + shutdown), `spdyForwarderFactory` (SPDY forward to one pod's `portforward` subresource, binds `127.0.0.1`, discards all traffic/metadata), the `forwarder` interface + `withForwarderFactory` test injection, and `startRequest{Namespace,Pod,RemotePort,LocalPort}`.
4. `internal/resources/services.go` — the Story 7.2 typed service detail already resolves a Service's endpoints (ready/not-ready) to pod links. Reuse that to enumerate ready backend pods + their target ports.

## Sprint goal
A port-forward whose target may be a **Service**: the backend opens one forward per ready endpoint pod and load-balances each new TCP connection across them — per-connection (the same granularity as ClusterIP/kube-proxy), **not** per-request/L7.

## Stories

### Story 1.1 — Service → ready-endpoints resolution
Resolve `(namespace, service, servicePort)` to the set of **ready** endpoint pods and each pod's concrete target port (resolving a named `targetPort` per pod). Reuse/extend `services.go`.
**Acceptance criteria:**
- [ ] Given a Service + one of its ports, return the ready backend pods with each pod's numeric container port (named `targetPort` → number resolved per pod).
- [ ] Zero ready endpoints ⇒ a fast, classified error (not a hang); Service or port not found ⇒ 404-class error.
- [ ] Only `Ready` endpoints are used; not-ready/terminating pods are excluded.

### Story 1.2 — Multi-forward + per-connection balancer
Open N per-pod SPDY forwards (reuse `spdyForwarderFactory`), each on its own ephemeral loopback port, fronted by one `127.0.0.1` listener that round-robins each **new connection** across the live backends.
**Acceptance criteria:**
- [ ] One public loopback listener; each accepted TCP connection is spliced (bidirectional `io.Copy`) to the next healthy backend (round-robin).
- [ ] A backend that dies (pod deleted mid-forward) drops from rotation; the session survives while ≥1 backend is live; the last backend gone ⇒ the session closes the way a dead pod forward does today.
- [ ] No forwarded bytes or connection metadata are ever logged (match the existing `io.Discard` posture).

### Story 1.3 — API + session lifecycle
Extend the port-forward API to accept a Service target, tracked as one session in `PortForwardManager`.
**Acceptance criteria:**
- [ ] The create request accepts a discriminated target — pod (today) **or** service `{namespace, service, servicePort, localPort}` — validated (mutually exclusive; clear 400 on bad input).
- [ ] The `PortForward` API view gains the target kind and, for a service, the service name + live backend count; `List` shows service forwards alongside pod forwards.
- [ ] Group teardown is unchanged: a context switch (`CloseOthers`) and shutdown (`CloseAll`) tear down all per-pod forwards of a service session together; the read-only guard on create is unchanged.
- [ ] **Scope guard:** the MVP **snapshots** ready endpoints at start; live EndpointSlice churn (pods added/removed after start) is an explicit follow-up recorded in the ADR — not this sprint.

### Story 1.4 — UI
Surface the load-balanced forward and show it in the active-forwards list.
**Acceptance criteria:**
- [ ] The Service detail view (or its port-forward control) offers "Port-forward (load-balanced across N endpoints)"; reuse `web/src/components/port-forward-controls.tsx`.
- [ ] `active-forwards-panel.tsx` shows service forwards with the service name + backend count; stop works identically.
- [ ] Read-only mode and setup-not-ready states behave like the existing pod-forward controls.

### Story 1.5 — ADR-0006 addendum
**Acceptance criteria:**
- [ ] Addendum to ADR-0006: the forward model extends 1:1-pod → 1:N-service; per-connection (not per-request/L7) balancing and why; snapshot-at-start with churn-tracking deferred; **no new Go dependency** (stdlib `net` + existing `spdy`/`portforward`). ADR index + `STATUS.md` updated.

## Task checklist
- [ ] Endpoint resolver (1.1) + tests.
- [ ] Balancer + multi-forward (1.2) using fake forwarders via `withForwarderFactory`.
- [ ] Target discrimination + manager session (1.3).
- [ ] Service-forward UI (1.4).
- [ ] ADR-0006 addendum + STATUS.

## Testing requirements
- Unit (Go): endpoint resolution (named targetPort, no-ready-endpoints, not-found); balancer round-robin distribution + backend-drop/rebalance using injected fake forwarders (mirror `portforward_test.go`); request validation (pod vs service mutual exclusion); teardown-on-context-switch for a service session.
- FE (vitest): the service-forward control + active-forwards row (backend count) + read-only/gated states.
- Manual kind smoke (testenv `web` namespace): start a load-balanced forward to the `frontend` Service (3× nginx), issue many requests and confirm they land across **multiple** pods (scale to 6 and observe distribution, or use a per-pod marker); stop and context-switch both tear it down.

## Definition of Done
- Compiles/builds; `make test` + `make lint` + `make fe-test` green; manual kind smoke above.
- ADR-0006 addendum written; `STATUS.md` updated (FB-13 done); no new Go dep; no change to the canonical `KUBESCOPE_*` set.

## End-of-session actions
1. `make test`, `make lint`, `make fe-test` + the manual kind smoke.
2. Update `STATUS.md` (last work `[sprint]` — v2 Sprint 1 / FB-13, next expected, checkboxes, ADR).
3. Commit (Conventional Commits), push the branch, open a PR; agent-review the diff and fix findings.
4. Gates green → squash-merge, sync `main`; concise summary.
