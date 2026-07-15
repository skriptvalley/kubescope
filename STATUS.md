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
- Last work: Sprint 5 — Mutations + guardrails [sprint]
- Summary: Write operations shipped, every one behind a confirmation dialog and the ADR-0005 guardrails. Backend: generic apply (`PUT /resources/{g}/{v}/{r}/{name}`) via the dynamic client for any GVK incl. CRDs — body is `{"yaml":...}` so validation is server-side; a stale resourceVersion is surfaced as a 409 (never a silent overwrite), invalid YAML a 400, server validation a 422 (new `writeMutationError` classifies conflict/invalid/already-exists before delegating). Generic delete (`DELETE …`, namespaced or cluster-scoped). Scale via the scale subresource (`POST /workloads/{r}/{ns}/{name}/scale`, Deploy/STS/RS); rollout-restart stamping the `kubectl.kubernetes.io/restartedAt` pod-template annotation (`…/restart`, Deploy/STS/DS). Node cordon/uncordon patch `spec.unschedulable`; drain (`…/drain`) cordons then evicts via the eviction API, skipping DaemonSet/mirror/terminal pods and reporting each pod's outcome (evicted/skipped/blocked/error) — a PDB-blocked eviction (429) is reported, not swallowed. `KUBESCOPE_READ_ONLY=true` enforced by server middleware on a mutation route group: every mutating route returns 403 (a table test enumerates all seven; a direct curl is rejected the same as the UI), while reads and the in-memory context switch stay usable. `GET /api/v1/config` exposes `{readOnly, authMode}` to the FE. Secret data masked by default in get + raw-YAML (`**redacted**`, keys preserved), with per-key reveal via `GET /api/v1/secrets/{ns}/{name}/reveal?key=` (decoded plaintext, never logged). Frontend: CodeMirror 6 YAML editor with an edit/apply flow (confirm dialog, 409 → reload-and-retry banner, inline validation errors); a reusable typed-name confirmation dialog gating every mutation (delete/drain require typing the object/node name); scale/restart controls on controller views, delete from list + detail views, cordon/uncordon + drain (with a per-pod results modal) on the nodes view; a read-only banner + all mutation controls hidden when read-only; masked Secret detail with per-key reveal. Verified: `go test -race ./...` incl. envtest for apply/409-conflict/scale/delete/cordon/drain-eviction(+PDB-blocked)/secret-masking; `fe-test` 119; lint/vet/gofmt/typecheck clean; FE prod build embeds CodeMirror; real-binary curl smoke confirmed read-only 403 on every mutating route and 200 config.
- Next expected: Sprint 6 — Exec terminal + port-forward
- ADRs touched this session: none (implements ADR-0005 guardrails + ADR-0003 generic apply/delete). Added the CodeMirror 6 frontend deps (`codemirror`, `@codemirror/lang-yaml`, `@codemirror/view`, `@codemirror/state`) — a pre-declared choice in the CLAUDE.md tech stack (YAML editor, Sprint 5), not a new decision.

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

### Sprint 5 — Mutations + guardrails — [done]
- Story 5.1 — Edit YAML + apply (CodeMirror editor, server-side update, conflict surfacing)
  - [x] CodeMirror YAML editor
  - [x] Server-side update/apply
  - [x] Conflict surfacing
- Story 5.2 — Scale, rollout-restart, delete — with typed confirmation dialogs
  - [x] Scale + rollout-restart endpoints
  - [x] Delete with typed confirmation dialogs
- Story 5.3 — Node cordon/uncordon/drain
  - [x] Cordon/uncordon endpoints
  - [x] Drain with confirmation
- Story 5.4 — `KUBESCOPE_READ_ONLY` enforcement (server middleware + UI state) + Secret masking
  - [x] Server middleware rejecting mutations in read-only mode
  - [x] UI read-only state
  - [x] Secret masking (reveal-on-click)

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
- [ ] FB-1: `writeEngineError` collapses every non-NotFound/Forbidden apiserver error to `502 cluster_unreachable` (+ADR-0004 guidance); a genuine apiserver 5xx/conflict is then mislabeled. Sprint 5 addressed this on the write paths — `writeMutationError` classifies Conflict→409, Invalid/BadRequest→422 and AlreadyExists→409 before delegating, so apply/scale/delete/cordon/drain surface those faithfully. The generic *read* engine still routes through `writeEngineError` (reads rarely conflict); revisit only if a read path needs finer 5xx taxonomy. (source: sprint-2 review, priority: lo)
