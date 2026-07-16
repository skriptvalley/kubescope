# Kubescope — Project Status

## How to update this file
- Update at the END of every session (rule in CLAUDE.md).
- Set "Last work" to what was completed + its type: [sprint] or [feedback].
- Set "Next expected" to the next sprint or the top feedback task.
- Tick task checkboxes as stories/tasks complete; mark sprint status.
- Log any new review/feedback items under "Feedback / Review Tasks".
- Record any ADR added/changed this session.

## Current state
- Last updated: 2026-07-16
- Last work: Sprint 7 — Config/networking/RBAC/storage + polish [sprint]
- Summary: Resource-appropriate views for config, networking, RBAC and storage, plus global search and a polish pass. **Secrets (7.1):** masking already enforced server-side (Sprint 5) at get/yaml/stream; the kind smoke surfaced a leak the envtest missed — `kubectl apply` records the full manifest (data included) in the `last-applied-configuration` annotation, which `maskSecretData` did not touch, so a Secret value survived the masked get/YAML despite `data` being redacted. Fixed: `maskSecretData` now also redacts that annotation (targeted, other annotations preserved); regression covered by a unit test + the sprint-7 envtest (which now seeds the annotation). ConfigMap/Secret detail views render data keys (long ConfigMap values collapsible; Secret values masked with per-key reveal-on-click). **List-column enrichment (7.1–7.4):** ADR-0003 keeps list columns server-side, so the generic engine now emits per-kind extra columns (`listRow.cells`, keyed by column id) for configmaps/secrets/services/ingresses/PVC/PV/storageclasses/serviceaccounts/roles/clusterroles/(cluster)rolebindings — computed in one central registry (`columns.go`) wired into both the REST list (`shapeList`) and the watch→SSE row shaper (`genericStreamRow`) so live updates carry the identical shape. Secret enrichment reads only key *counts* + type, never a value. The thin frontend renders any extra column via `ResourceRow.cells` (default table cell). **Services/Ingress (7.2):** typed Service detail endpoint `GET /api/v1/services/{ns}/{name}` resolves Endpoints into ready/not-ready backing addresses (each linked to its pod via targetRef — the Service's matching pod list); Ingress detail renders rules (host/path/backend) + TLS from the object, backends cross-linking to their Service, TLS to its Secret. **RBAC (7.3):** Role/ClusterRole rules table, RoleBinding/ClusterRoleBinding subjects + roleRef (linked to the referenced role; cluster vs namespaced routes), ServiceAccount secrets/imagePullSecrets — all from the object; nav separation is discovery-driven. **Storage (7.4):** PVC (status/capacity/access-modes/class/bound-PV linked), PV (reclaim/capacity/phase/claimRef linked), StorageClass (provisioner/default-marker) — PVC⇄PV cross-links; pending/unbound render a meaningful status. **Search + polish (7.5):** `GET /api/v1/search?q=&limit=` name-matches across the active context's discovered types (bounded ≤100, per-type-failure → warning for partial results, opaque high-volume kinds skipped); global-search UI (`/` focuses, arrow keys move selection, Enter/click navigates, Esc closes, debounced) + a shortcuts-help popover (`?`); shared `EmptyState`/`ErrorState` (ApiError code→title, retry action) replace the per-file copies across every list/detail. Verified: full `go test -race ./...` incl. envtest green; `fe-test` 172; lint/vet/gofmt/eslint/tsc clean; FE prod build embeds; **real kind smoke** — seeded ConfigMap/Secret/Service+Ingress/PVC/RBAC in a fresh namespace: masked get/YAML leak-count 0 (annotation fix), per-key reveal decodes; service detail resolves the backing pod endpoint; enriched columns correct for every kind; global search finds each object (secret value not leaked in results); empty namespace lists 0 rows; read-only DELETE → 403 while reveal → 200; browser-verified reveal-on-click, search navigate-on-select, Ingress→Service and Service→pod cross-links.
- Next expected: Sprint 8 — Hardening & release
- ADRs touched this session: none. No new dependencies, no architecture/locked-decision changes. The list-column enrichment applies ADR-0003's "list-column config lives server-side per resource" (adds a `cells` map to the existing row shape); the typed Service detail endpoint is an ADR-0003 hot-path handler; the Secret annotation masking is a fix within the ADR-0005 posture (no policy change).

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

### Sprint 6 — Exec terminal + port-forward — [done]
- Story 6.1 — WebSocket exec bridge (backend: coder/websocket ⇆ SPDY exec)
  - [x] coder/websocket ⇆ SPDY exec bridge
  - [x] Session lifecycle + cleanup
- Story 6.2 — xterm.js terminal UI (container select, resize, reconnect)
  - [x] Terminal with container select
  - [x] Resize + reconnect handling
- Story 6.3 — Port-forward (start/stop, list active forwards)
  - [x] Start/stop port-forward API
  - [x] Active forwards list

### Sprint 7 — Config/networking/RBAC/storage + polish — [done]
- Story 7.1 — ConfigMaps + Secrets (masked by default, reveal-on-click)
  - [x] ConfigMap views
  - [x] Secret views masked by default, reveal-on-click (+ last-applied annotation leak fixed)
- Story 7.2 — Services + Ingress views
  - [x] Service views (typed detail: endpoints ready/not-ready → pod links)
  - [x] Ingress views (rules + TLS, backend→service cross-links)
- Story 7.3 — RBAC: Roles/ClusterRoles, Bindings, ServiceAccounts
  - [x] Roles/ClusterRoles + Bindings views (rules table, roleRef link)
  - [x] ServiceAccount views
- Story 7.4 — Storage: PV, PVC, StorageClass
  - [x] PV + PVC views (PVC⇄PV cross-links, meaningful pending status)
  - [x] StorageClass views (provisioner + default marker)
- Story 7.5 — Global search + empty/error states + keyboard nav
  - [x] Global search (name match across discovered types, bounded, partial-tolerant)
  - [x] Empty/error states pass (shared EmptyState/ErrorState with retry)
  - [x] Keyboard navigation (/ focus, arrows, Esc, ? help popover)

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
- [ ] FB-3: No Host-header allowlist on the API, so a DNS-rebinding page could reach a writable localhost instance (exec, mutations) despite the WebSocket Origin check — Origin-only checks can't stop rebinding, since the same-origin branch trusts `Host==Origin`. App-wide (every endpoint), not exec-specific; current mitigations are the loopback default bind, `KUBESCOPE_READ_ONLY`, and (soon) auth. Fix in the Sprint 8 security pass: a Host-allowlist middleware (configured listen addr + localhost/127.0.0.1[:port]) ahead of the API tree. (source: sprint-6 review, priority: med)
- [x] FB-2: Context switch left the currently-mounted view (e.g. Overview) showing the prior cluster's data until a manual refresh or navigation. `useSwitchContext` removed cluster-scoped queries before the global invalidate, and a removed active query has no observer to refetch. Fixed: invalidate first (refetches mounted views in place), then drop only `type:"inactive"` caches. Regression test added; verified in-browser against two kind clusters. (source: manual testing, priority: hi) [done]
- [ ] FB-1: `writeEngineError` collapses every non-NotFound/Forbidden apiserver error to `502 cluster_unreachable` (+ADR-0004 guidance); a genuine apiserver 5xx/conflict is then mislabeled. Sprint 5 addressed this on the write paths — `writeMutationError` classifies Conflict→409, Invalid/BadRequest→422 and AlreadyExists→409 before delegating, so apply/scale/delete/cordon/drain surface those faithfully. The generic *read* engine still routes through `writeEngineError` (reads rarely conflict); revisit only if a read path needs finer 5xx taxonomy. (source: sprint-2 review, priority: lo)
