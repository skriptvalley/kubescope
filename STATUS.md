# Kubescope — Project Status

## How to update this file
- Update at the END of every session (rule in CLAUDE.md).
- Set "Last work" to what was completed + its type: [sprint] or [feedback].
- Set "Next expected" to the next sprint or the top feedback task.
- Tick task checkboxes as stories/tasks complete; mark sprint status.
- Log any new review/feedback items under "Feedback / Review Tasks".
- Record any ADR added/changed this session.

## Current state
- Last updated: 2026-07-15
- Last work: Sprint 4 — Live updates + logs + events [sprint]
- Summary: The dashboard is now live (ADR-0006 SSE). New `internal/stream`: a Hub owning one shared dynamic informer per (context, GVR), ref-counted and torn down when the last subscriber disconnects; per-subscriber handlers give each stream the informer's initial snapshot as adds, then live add/update/delete. Watch errors raise an explicit `resync`; a slow subscriber's buffer overflow raises a resync rather than blocking; heartbeat comments keep idle streams alive and detect a context switch to close streams bound to the prior context. Events carry the same server-shaped row the REST list/feed returns (`resources.ShapeStreamRow`: typed summaries for the seven workload kinds, an event-feed row for core Events, else the generic metadata row; detail subscribers also get the full object). Routes: `GET /api/v1/stream/resources/{group}/{version}/{resource}?namespace=&name=&detail=`, `GET /api/v1/stream/pods/{ns}/{name}/logs?container=&follow=&previous=&tailLines=`, and `GET /api/v1/events/feed?namespace=&type=` (initial-paint + polling fallback). UI: a reconnecting EventSource client (exponential backoff, live/stale indicator) feeds live-update hooks that patch the TanStack Query cache in place — no full refetch — with polling fallback while a stream is not live; wired into generic + typed workload lists and the generic/pod detail object (deletion of the viewed object surfaced). New Pod "Logs" tab (container select, follow, previous, tail lines; auto-scroll that pauses on scroll-up, resumable; closed-state surfaced). New live Events page (namespace + type filters, deep-links to the involved object) in the sidebar. Manual kind smoke: watched pods while scaling a Deployment (add/update/delete with typed rows), streamed follow + tail logs and validated the closed-event, hit the events feed while force-deleting a pod, tailLines=-5 → 400, zero panics under watch churn. `make test` (race+envtest incl. hub fan-out/ref-counting/namespace-filter/resync + watch-delivery envtest), lint and build all green; fe-test 104. Post-implementation adversarial multi-agent review (9 confirmed findings) applied: fixed a log-reader goroutine leak on client disconnect (ctx-aware send), resync now drains stale buffered events, EventFeedRow made a superset of the generic row (uid+creationTimestamp), detail `deleted` flag resets on object switch + object view gains the poll fallback, log stream clears on reconnect (no dup lines), events racing the initial baseline flush via a deferred refetch; added heartbeat + watch-error-resync tests.
- Next expected: Sprint 5 — Mutations + guardrails
- ADRs touched this session: none (implements ADR-0006 SSE watch/log bridge as specified)

## Sprint board

### Sprint 0 — Walking skeleton & deployment spine — [done]
- Story 0.1 — Go module & HTTP server skeleton
  - [x] Init module `github.com/skriptvalley/kubescope`; entrypoint in `cmd/kubescope`
  - [x] chi router + slog + `/healthz`
  - [x] Env config loading/validation in `internal/config` (`KUBESCOPE_*`)
- Story 0.2 — Frontend scaffold
  - [x] Vite + React 18 + TS app in `web/`
  - [x] Tailwind + shadcn/ui setup
  - [x] TanStack Query + react-router wiring
- Story 0.3 — Single binary: embed built FE via `embed.FS`, SPA fallback serving
  - [x] Embed built frontend via `embed.FS`
  - [x] SPA fallback serving from the Go server
- Story 0.4 — Prove the model: load mounted kubeconfig, list nodes via client-go, render node list in UI
  - [x] Load kubeconfig from `KUBESCOPE_KUBECONFIG` (with fallbacks)
  - [x] Node list API via client-go
  - [x] Render node list in the UI
- Story 0.5 — Multi-stage multi-arch Dockerfile + real Makefile targets + kind config in deploy/
  - [x] Multi-stage Dockerfile (node → go → minimal runtime)
  - [x] Multi-arch build (amd64 + arm64)
  - [x] Makefile targets + kind config in `deploy/`

### Sprint 1 — Kubeconfig & context management + cluster overview — [done]
- Story 1.1 — Kubeconfig parsing & context enumeration API
  - [x] Parse mounted kubeconfig; enumerate contexts
  - [x] Contexts API endpoint
- Story 1.2 — Context switching + per-context rest.Config/client cache
  - [x] Context switch API + UI switcher
  - [x] Per-context rest.Config/client cache
- Story 1.3 — Per-context connection/health status
  - [x] Reachability + auth check per context
  - [x] Server version probe; status surfaced in UI
- Story 1.4 — Cluster overview page
  - [x] Overview API (server version, node count, namespace list)
  - [x] Overview page UI

### Sprint 2 — Generic resource engine (read-only) — [done]
- Story 2.1 — Discovery: enumerate all API groups/resources incl. CRDs (cached, refreshable)
  - [x] Discovery of all groups/resources incl. CRDs
  - [x] Caching + manual refresh
- Story 2.2 — Dynamic client get/list; cluster- vs namespace-scoped handling; namespace selector API
  - [x] Dynamic get/list for any GVK
  - [x] Cluster- vs namespace-scoped handling
  - [x] Namespace selector API
- Story 2.3 — Generic resource list UI (TanStack Table, sidebar nav built from discovery, namespace selector)
  - [x] TanStack Table generic list
  - [x] Sidebar nav built from discovery
  - [x] Namespace selector UI
- Story 2.4 — Generic resource detail view + raw YAML tab
  - [x] Generic detail view
  - [x] Raw YAML tab

### Sprint 3 — Workload deep views — [done]
- Story 3.1 — Typed backend summaries for Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs
  - [x] Typed summary endpoints for the seven workload kinds
  - [x] Table-driven tests for summaries
- Story 3.2 — Pod detail: containers, statuses, restarts, conditions, node placement
  - [x] Containers, statuses, restarts panels
  - [x] Conditions + node placement
- Story 3.3 — Controller views: replicas, rollout status, owned-pod lists
  - [x] Replica + rollout status views
  - [x] Owned-pod lists
- Story 3.4 — Related events on workload detail views
  - [x] Per-object events API
  - [x] Events panel on detail views

### Sprint 4 — Live updates + logs + events — [done]
- Story 4.1 — Watch→SSE bridge (informers, per-context fan-out, reconnect handling)
  - [x] Informer-backed watch per context
  - [x] SSE fan-out endpoint
  - [x] Reconnect handling
- Story 4.2 — Live-updating lists/details in UI (SSE consumption → TanStack Query cache updates)
  - [x] SSE consumption in the frontend
  - [x] TanStack Query cache updates from events
- Story 4.3 — Pod log streaming (follow, container select, previous, tail lines)
  - [x] Log stream endpoint (follow, previous, tail lines)
  - [x] Log viewer UI with container select
- Story 4.4 — Events feed (cluster-wide + per-namespace)
  - [x] Events feed API
  - [x] Events feed UI with namespace filter

### Sprint 5 — Mutations + guardrails — [todo]
- Story 5.1 — Edit YAML + apply (CodeMirror editor, server-side update, conflict surfacing)
  - [ ] CodeMirror YAML editor
  - [ ] Server-side update/apply
  - [ ] Conflict surfacing
- Story 5.2 — Scale, rollout-restart, delete — with typed confirmation dialogs
  - [ ] Scale + rollout-restart endpoints
  - [ ] Delete with typed confirmation dialogs
- Story 5.3 — Node cordon/uncordon/drain
  - [ ] Cordon/uncordon endpoints
  - [ ] Drain with confirmation
- Story 5.4 — `KUBESCOPE_READ_ONLY` enforcement (server middleware + UI state) + Secret masking
  - [ ] Server middleware rejecting mutations in read-only mode
  - [ ] UI read-only state
  - [ ] Secret masking (reveal-on-click)

### Sprint 6 — Exec terminal + port-forward — [todo]
- Story 6.1 — WebSocket exec bridge (backend: coder/websocket ⇆ SPDY exec)
  - [ ] coder/websocket ⇆ SPDY exec bridge
  - [ ] Session lifecycle + cleanup
- Story 6.2 — xterm.js terminal UI (container select, resize, reconnect)
  - [ ] Terminal with container select
  - [ ] Resize + reconnect handling
- Story 6.3 — Port-forward (start/stop, list active forwards)
  - [ ] Start/stop port-forward API
  - [ ] Active forwards list

### Sprint 7 — Config/networking/RBAC/storage + polish — [todo]
- Story 7.1 — ConfigMaps + Secrets (masked by default, reveal-on-click)
  - [ ] ConfigMap views
  - [ ] Secret views masked by default, reveal-on-click
- Story 7.2 — Services + Ingress views
  - [ ] Service views
  - [ ] Ingress views
- Story 7.3 — RBAC: Roles/ClusterRoles, Bindings, ServiceAccounts
  - [ ] Roles/ClusterRoles + Bindings views
  - [ ] ServiceAccount views
- Story 7.4 — Storage: PV, PVC, StorageClass
  - [ ] PV + PVC views
  - [ ] StorageClass views
- Story 7.5 — Global search + empty/error states + keyboard nav
  - [ ] Global search
  - [ ] Empty/error states pass
  - [ ] Keyboard navigation

### Sprint 8 — Hardening & release — [todo]
- Story 8.1 — Optional auth: basic-auth toggle (OIDC if time permits) via `KUBESCOPE_AUTH_MODE`
  - [ ] Basic-auth toggle via `KUBESCOPE_AUTH_MODE`
  - [ ] OIDC if time permits
- Story 8.2 — Security pass: finalize read-only enforcement, secret-handling audit
  - [ ] Finalize read-only enforcement
  - [ ] Secret-handling audit (no secret values logged)
- Story 8.3 — CI + multi-arch image publish (lint/test/build on PR; image on tag)
  - [ ] CI: lint/test/build on PR
  - [ ] Multi-arch image publish on tag
- Story 8.4 — v0.1.0: tag, GitHub release, docs pass, optional Helm chart
  - [ ] Tag v0.1.0 + GitHub release
  - [ ] Docs pass
  - [ ] Optional Helm chart

## v2 backlog
- [ ] Resource graph
- [ ] Metrics (metrics-server)
- [ ] Multi-cluster side-by-side
- [ ] Plugin system

## Feedback / Review Tasks
<!-- Format: - [ ] FB-<n>: <description> (source: <sprint/review>, priority: <hi/med/lo>) -->
- [x] FB-2: Context switch left the currently-mounted view (e.g. Overview) showing the prior cluster's data until a manual refresh or navigation. `useSwitchContext` removed cluster-scoped queries before the global invalidate, and a removed active query has no observer to refetch. Fixed: invalidate first (refetches mounted views in place), then drop only `type:"inactive"` caches. Regression test added; verified in-browser against two kind clusters. (source: manual testing, priority: hi) [done]
- [ ] FB-1: `writeEngineError` collapses every non-NotFound/Forbidden apiserver error to `502 cluster_unreachable` (+ADR-0004 guidance); a genuine apiserver 5xx/conflict is then mislabeled. Kept consistent with the existing overview handler for now — revisit the error taxonomy when mutations land (Sprint 5). (source: sprint-2 review, priority: lo)
