# Kubescope — Project Status

## How to update this file
- Update at the END of every session (rule in CLAUDE.md).
- Set "Last work" to what was completed + its type: [sprint] or [feedback].
- Set "Next expected" to the next sprint or the top feedback task.
- Tick task checkboxes as stories/tasks complete; mark sprint status.
- Log any new review/feedback items under "Feedback / Review Tasks".
- Record any ADR added/changed this session.

## Current state
- Last updated: 2026-07-17
- Last work: FB-8, FB-9 — kubeconfig source registry & onboarding polish [feedback]
- Summary: FB-8/FB-9 mini-sprint (`prompts/feedback-kubeconfig-registry-and-onboarding-polish.md`) complete, recorded in **ADR-0008** (supersedes 0007). **Story A:** `KUBESCOPE_KUBECONFIG` (and the `$KUBECONFIG` fallback) now accepts an OS-path-list of files **and directories** (`filepath.SplitList`, empty segments dropped; `Config.KubeconfigSources []string`); the default `/kubeconfig` probe accepts a directory, so `-v ~/.kube:/kubeconfig:ro` works with zero env vars; `kube.Manager` holds a `[]registrySource` and loads via clientcmd **Precedence** (kubectl merge — first occurrence of a name wins, current-context from the first file that sets one); `expandSources` (`internal/kube/sources.go`) stats each source per request, expands dirs non-recursively/lexicographically with per-file classified statuses (`ok|unparseable|too_large|hidden`, subdirs skipped), computes contributed/shadowed context names by a first-wins walk, and zero usable files is a typed `NoUsableSourceError`. **Story B:** `GET/POST /api/v1/kubeconfigs` + `DELETE /api/v1/kubeconfigs/{id}` (id = 12-hex sha256 of path) replace `PUT /api/v1/kubeconfig` (pre-v0.1.0, no compat); GET is unguarded like `/setup`; POST/DELETE sit inside the read-only group and keep the `KUBESCOPE_ALLOW_KUBECONFIG_SET` handler gate (403 `kubeconfig_set_disabled`); codes: 400 bad path, 409 `kubeconfig_source_exists`, 422 `kubeconfig_invalid` (invisible path ⇒ mounted-dir guidance + ADR-0004 link), 404 `kubeconfig_source_not_found`; every mutation validates-before-commit inside one write-lock critical section (no lost updates under concurrent add/remove), resets the client cache, bumps `SourceGeneration`, fires the source observer — the active override is kept and `resolveActive` self-heals, so removing the active source falls back gracefully (remaining sources' current-context, else `no_active_context`); `SetupState.kubeconfigPath` → `kubeconfigSources []string`, and the `no_kubeconfig` reason split now derives from per-source statuses (an unparseable file inside an otherwise-empty dir reads `kubeconfig_invalid`, not "mount one"). **Story C:** dir expansion is per-request (no watcher, failures uncached), so dropping/removing a file in a mounted dir registers/degrades without restart; UI Rescan is a pure refetch; README documents the mount-once workflow. **Story D:** ADR-0008 (registry model, kubectl merge, in-memory overlay/restart-reverts, scan rules, Docker runtime-mount analysis, paste/upload re-rejected, generation-on-mutation-only with probe-eviction covering passive winner changes); ADR-0007 re-statused superseded; CLAUDE.md canonical-env note updated (no new env var). **Story E (FB-9):** sidebar and context switcher call `useSetupState()` and render muted placeholders ("Waiting for a cluster connection…" / "no cluster") instead of red errors whenever setup ≠ ready (loading counts as non-ready); genuine mid-session errors (setup ready) stay red byte-for-byte; new FE surface `kubeconfig-sources.tsx` (list + per-file rows + shadowed notes + add/remove/rescan, `canSetKubeconfig`-gated) reused by all four starter variants and a "Manage kubeconfig sources" dialog off the context-switcher menu. Verified: `make test` (race + envtest) green incl. new sources/registry/handler/setup tests; `fe-test` 219 (46 files); lint + tsc clean; `make build`; **binary smoke against two kind clusters** covering the full scenario matrix — separate per-cluster files in one dir (both contexts, broken file per-file `unparseable`, no global failure), colon list, single-file zero-regression, shadowing surfaced (earlier lexicographic file wins), drop-in/drop-out live without restart, UI add of visible path (starter flips to ready in-browser), invisible path 422 + mounted-dir guidance, duplicate 409, relative 400, unknown-id 404, remove-active-source fallback to remaining current-context, empty registry → `no_kubeconfig` starter, restart reverts to env baseline, read-only POST 403 `read_only` (GET still works), flag-off 403 `kubeconfig_set_disabled`, FB-9 muted sidebar/switcher confirmed in-browser, switcher menu → manage dialog with per-file statuses. Review hardening (own pass + 42-agent adversarial review of the diff, 6 dimensions × 3-vote verification; 12 candidate findings, 4 confirmed, all fixed): registry mutations rebuilt against the current registry inside the commit lock (closed a concurrent add/remove lost-update); broken-only-dir reason fixed to `kubeconfig_invalid`; **dir-entry symlinks now judged by their target** (`os.Stat` + regular-file check — a symlink's lstat size let an oversized/`/dev/zero`-style target through the 1MiB cap into an unbounded `LoadFromFile` read; dir-target links skipped, dangling links per-file status); **source mutations/rescan now mirror the FB-2 context-switch invalidation** (global invalidate then drop inactive cluster-scoped caches — removing the source that repoints the active context no longer strands Overview/list views on the previous cluster's data); ADR-0008 DELETE shape corrected to `/{id}`; ARCHITECTURE.md env table de-staled (PUT/0007 → registry/0008). Tests added for each.
- Next expected: Cut v0.1.0 release (push `v0.1.0` tag → release workflow publishes the image → GitHub release + smoke the published image via the canonical docker run) — pending go-ahead; then FB-5/FB-4 (both lo) or new feedback.
- ADRs touched this session: 0008 (new — kubeconfig source registry: files + directories, kubectl merge precedence, in-memory overlay, paste/upload re-rejected); 0007 re-statused Superseded by 0008. No new dependency. (Previous session: 0007 new — runtime kubeconfig source, path-only, opt-in.)

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

### Sprint 8 — Hardening & release — [in-progress]
- Story 8.1 — Optional auth: basic-auth toggle (OIDC if time permits) via `KUBESCOPE_AUTH_MODE`
  - [x] Basic-auth toggle via `KUBESCOPE_AUTH_MODE` (creds via `KUBESCOPE_AUTH_BASIC_USERNAME`/`_PASSWORD`; `oidc` fails fast → FB-5)
  - [ ] OIDC if time permits (skipped — stretch; fails fast at startup as required → FB-5)
- Story 8.2 — Security pass: finalize read-only enforcement, secret-handling audit
  - [x] Finalize read-only enforcement (enumerated + `chi.Walk` route-surface regression; FB-3 Host-allowlist)
  - [x] Secret-handling audit (no secret values logged; all data paths swept)
- Story 8.3 — CI + multi-arch image publish (lint/test/build on PR; image on tag)
  - [x] CI: lint/test/build on PR (`.github/workflows/ci.yml`; green pending first GitHub run on the PR)
  - [x] Multi-arch image publish on tag (`.github/workflows/release.yml`; fires on `v*`)
- Story 8.4 — v0.1.0: tag, GitHub release, docs pass, optional Helm chart
  - [ ] Tag v0.1.0 + GitHub release (pending go-ahead — outward-facing; commands ready)
  - [x] Docs pass (README, ADR-0005 addendum, env-var table, ADR-0004 gotchas, CHANGELOG)
  - [ ] Optional Helm chart (skipped → FB-4)

## v2 backlog
- [ ] Resource graph
- [ ] Metrics (metrics-server)
- [ ] Multi-cluster side-by-side
- [ ] Plugin system

## Feedback / Review Tasks
<!-- Format: - [ ] FB-<n>: <description> (source: <sprint/review>, priority: <hi/med/lo>) -->
- [x] FB-9: Onboarding polish — with no usable kubeconfig, the sidebar leaks a raw red "discovering resources: loading kubeconfig …" error and the context switcher shows red "kubeconfig error" alongside the starter page (which owns that messaging). Suppress both behind the setup gate: muted/neutral states when setup ≠ ready; unchanged when ready. Captured as Story E in `prompts/feedback-kubeconfig-registry-and-onboarding-polish.md`. (source: user screenshot post-FB-6, priority: med) [done — both surfaces key off `useSetupState()`; muted placeholders pre-ready, red preserved when ready; covered by new sidebar + switcher vitest cases and in-browser smoke]
- [x] FB-8: Kubeconfig source registry — accept multiple kubeconfig sources (colon-separated files AND directories in `KUBESCOPE_KUBECONFIG`, kubectl merge precedence via clientcmd); registry API + UI (starter + context-switcher menu) to add/remove sources at runtime (in-memory overlay, restart reverts; supersedes the FB-6 single `PUT /api/v1/kubeconfig` — pre-v0.1.0, no compat burden); directory re-scan makes "drop a file into a mounted dir" register a new cluster without restart (the only safe runtime-add in Docker — runtime mounts don't exist and socket/privileged workarounds are rejected under ADR-0005; Kubescope also can't copy files it can't read nor write the read-only mount). Requires ADR-0008 (registry model + explicit paste/upload re-decision). Full plan: `prompts/feedback-kubeconfig-registry-and-onboarding-polish.md`. (source: user, priority: med) [done — ADR-0008, merged Precedence loading + per-file statuses/shadowing, `GET/POST /api/v1/kubeconfigs` + `DELETE /{id}`, sources UI on starter + switcher dialog, full scenario-matrix kind smoke]
- [x] FB-7: Dev-tooling — `deploy/testenv/testenv.sh` only has a bare-binary `run`; add a `run --docker` (and/or `make testenv-run-docker`) that rewrites the isolated testenv kubeconfig for a container (127.0.0.1→host.docker.internal, insecure-skip-tls-verify, both contexts, read-only mount) and runs the image. Captured as Story E in `prompts/feedback-cluster-connectivity-and-onboarding.md`. (source: user, priority: lo) [done — `run --docker` + `make testenv-run`/`testenv-run-docker`; per-OS derivation, trap-cleaned temp copy]
- [x] FB-6: Cluster-connectivity resilience & onboarding — never dead-end on a cluster problem. No-kubeconfig/no-context/unreachable should render a guided starter page (not a raw red error); classify apiserver/transport failures into a taxonomy with inline fix suggestions (absorbs FB-1); let users set/reload the kubeconfig from the UI (gated, needs an ADR — powerful capability under ADR-0004/0005); and handle a cluster torn down while viewing over SSE (typed unreachable event + bounded backoff + auto-resume, no busy-loop). Full plan: `prompts/feedback-cluster-connectivity-and-onboarding.md`. (source: user/manual testing, priority: hi) [done — setup-state starter page, `kube.ClassifyError` taxonomy end to end, ADR-0007 runtime kubeconfig source, typed SSE status + prober/backoff + informer rebuild + probe→hub sync; kind smoke of every scenario-matrix row that a host binary can hit]
- [ ] FB-5: OIDC auth mode (`KUBESCOPE_AUTH_MODE=oidc`) — stretch, not implemented in v1; currently fails fast at startup. Land alongside multi-user/hashed (htpasswd/bcrypt) credentials as the richer auth story; in-container exec-plugin/SSO login relates to FB-6 Story C. (source: sprint-8, priority: lo)
- [ ] FB-4: Minimal Helm chart for in-cluster deployment — deferred. An in-cluster Helm install implies a ServiceAccount-based product shape, which ADR-0004 explicitly rejects for v1 (Kubescope is built around a mounted host kubeconfig + context switching). Revisit if in-cluster deployment becomes a goal; a chart that mounts a kubeconfig from a Secret is the plausible bridge. (source: sprint-8, priority: lo)
- [x] FB-3: No Host-header allowlist on the API, so a DNS-rebinding page could reach a writable localhost instance (exec, mutations) despite the WebSocket Origin check — Origin-only checks can't stop rebinding, since the same-origin branch trusts `Host==Origin`. App-wide (every endpoint), not exec-specific; current mitigations are the loopback default bind, `KUBESCOPE_READ_ONLY`, and (soon) auth. Fix in the Sprint 8 security pass: a Host-allowlist middleware (configured listen addr + localhost/127.0.0.1[:port]) ahead of the API tree. (source: sprint-6 review, priority: med) [done — `hostGuard` in `internal/server/hostcheck.go`; loopback + concrete-bind allowlist, `/healthz` exempt, wildcard binds pass through to auth+network controls; covered by `hostcheck_test.go` + binary smoke]
- [x] FB-2: Context switch left the currently-mounted view (e.g. Overview) showing the prior cluster's data until a manual refresh or navigation. `useSwitchContext` removed cluster-scoped queries before the global invalidate, and a removed active query has no observer to refetch. Fixed: invalidate first (refetches mounted views in place), then drop only `type:"inactive"` caches. Regression test added; verified in-browser against two kind clusters. (source: manual testing, priority: hi) [done]
- [x] FB-1: `writeEngineError` collapses every non-NotFound/Forbidden apiserver error to `502 cluster_unreachable` (+ADR-0004 guidance); a genuine apiserver 5xx/conflict is then mislabeled. Sprint 5 addressed this on the write paths — `writeMutationError` classifies Conflict→409, Invalid/BadRequest→422 and AlreadyExists→409 before delegating, so apply/scale/delete/cordon/drain surface those faithfully. The generic *read* engine still routes through `writeEngineError` (reads rarely conflict); revisit only if a read path needs finer 5xx taxonomy. (source: sprint-2 review, priority: lo) [done — absorbed into FB-6 Story B: reads now classify Conflict/Invalid/401/403/timeout/5xx/transport faithfully via the taxonomy]
