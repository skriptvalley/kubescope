# Kubescope — Project Status

## How to update this file
- Update at the END of every session (rule in CLAUDE.md).
- Set "Last work" to what was completed + its type: [sprint] or [feedback].
- Set "Next expected" to the next sprint or the top feedback task.
- Tick task checkboxes as stories/tasks complete; mark sprint status.
- Log any new review/feedback items under "Feedback / Review Tasks".
- Record any ADR added/changed this session.

## Current state
- Last updated: 2026-07-14
- Last work: Session 0 — Bootstrap (scaffolding, docs, ADRs, prompts, skills) [sprint]
- Summary: Repo bootstrapped: docs, ADRs 0001–0006, sprint prompts 0–8, skills, scaffold. No application code yet.
- Next expected: Sprint 0 — Walking skeleton & deployment spine
- ADRs touched this session: 0001–0006 (created)

## Sprint board

### Sprint 0 — Walking skeleton & deployment spine — [todo]
- Story 0.1 — Go module & HTTP server skeleton
  - [ ] Init module `github.com/skriptvalley/kubescope`; entrypoint in `cmd/kubescope`
  - [ ] chi router + slog + `/healthz`
  - [ ] Env config loading/validation in `internal/config` (`KUBESCOPE_*`)
- Story 0.2 — Frontend scaffold
  - [ ] Vite + React 18 + TS app in `web/`
  - [ ] Tailwind + shadcn/ui setup
  - [ ] TanStack Query + react-router wiring
- Story 0.3 — Single binary: embed built FE via `embed.FS`, SPA fallback serving
  - [ ] Embed built frontend via `embed.FS`
  - [ ] SPA fallback serving from the Go server
- Story 0.4 — Prove the model: load mounted kubeconfig, list nodes via client-go, render node list in UI
  - [ ] Load kubeconfig from `KUBESCOPE_KUBECONFIG` (with fallbacks)
  - [ ] Node list API via client-go
  - [ ] Render node list in the UI
- Story 0.5 — Multi-stage multi-arch Dockerfile + real Makefile targets + kind config in deploy/
  - [ ] Multi-stage Dockerfile (node → go → minimal runtime)
  - [ ] Multi-arch build (amd64 + arm64)
  - [ ] Makefile targets + kind config in `deploy/`

### Sprint 1 — Kubeconfig & context management + cluster overview — [todo]
- Story 1.1 — Kubeconfig parsing & context enumeration API
  - [ ] Parse mounted kubeconfig; enumerate contexts
  - [ ] Contexts API endpoint
- Story 1.2 — Context switching + per-context rest.Config/client cache
  - [ ] Context switch API + UI switcher
  - [ ] Per-context rest.Config/client cache
- Story 1.3 — Per-context connection/health status
  - [ ] Reachability + auth check per context
  - [ ] Server version probe; status surfaced in UI
- Story 1.4 — Cluster overview page
  - [ ] Overview API (server version, node count, namespace list)
  - [ ] Overview page UI

### Sprint 2 — Generic resource engine (read-only) — [todo]
- Story 2.1 — Discovery: enumerate all API groups/resources incl. CRDs (cached, refreshable)
  - [ ] Discovery of all groups/resources incl. CRDs
  - [ ] Caching + manual refresh
- Story 2.2 — Dynamic client get/list; cluster- vs namespace-scoped handling; namespace selector API
  - [ ] Dynamic get/list for any GVK
  - [ ] Cluster- vs namespace-scoped handling
  - [ ] Namespace selector API
- Story 2.3 — Generic resource list UI (TanStack Table, sidebar nav built from discovery, namespace selector)
  - [ ] TanStack Table generic list
  - [ ] Sidebar nav built from discovery
  - [ ] Namespace selector UI
- Story 2.4 — Generic resource detail view + raw YAML tab
  - [ ] Generic detail view
  - [ ] Raw YAML tab

### Sprint 3 — Workload deep views — [todo]
- Story 3.1 — Typed backend summaries for Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs
  - [ ] Typed summary endpoints for the seven workload kinds
  - [ ] Table-driven tests for summaries
- Story 3.2 — Pod detail: containers, statuses, restarts, conditions, node placement
  - [ ] Containers, statuses, restarts panels
  - [ ] Conditions + node placement
- Story 3.3 — Controller views: replicas, rollout status, owned-pod lists
  - [ ] Replica + rollout status views
  - [ ] Owned-pod lists
- Story 3.4 — Related events on workload detail views
  - [ ] Per-object events API
  - [ ] Events panel on detail views

### Sprint 4 — Live updates + logs + events — [todo]
- Story 4.1 — Watch→SSE bridge (informers, per-context fan-out, reconnect handling)
  - [ ] Informer-backed watch per context
  - [ ] SSE fan-out endpoint
  - [ ] Reconnect handling
- Story 4.2 — Live-updating lists/details in UI (SSE consumption → TanStack Query cache updates)
  - [ ] SSE consumption in the frontend
  - [ ] TanStack Query cache updates from events
- Story 4.3 — Pod log streaming (follow, container select, previous, tail lines)
  - [ ] Log stream endpoint (follow, previous, tail lines)
  - [ ] Log viewer UI with container select
- Story 4.4 — Events feed (cluster-wide + per-namespace)
  - [ ] Events feed API
  - [ ] Events feed UI with namespace filter

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
_None yet._
