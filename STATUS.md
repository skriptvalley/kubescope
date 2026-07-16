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
- Last work: FB-6, FB-1, FB-7 — cluster-connectivity resilience & onboarding [feedback]
- Summary: FB-6 mini-sprint (`prompts/feedback-cluster-connectivity-and-onboarding.md`) complete — Kubescope never dead-ends on a cluster problem. **Story A:** `GET /api/v1/setup` resolves a five-state posture (`no_kubeconfig | no_contexts | no_active_context | active_unreachable | ready` — `no_active_context` added beyond the prompt's four so "contexts exist, none selected" gets a picker, not an error); the SPA gates on it in `Layout` (`SetupGate`) and renders a guided `StarterPage` variant per state (mount/point instructions + ADR-0004 caveats, add-a-context help, one-click context picker, classified unreachable card) instead of the red overview error; `active_unreachable` only gates before the first successful connection (module `connectivity` store's `everConnected`), afterwards the in-app `ClusterBanner` handles it. **Story B (absorbs FB-1):** `kube.ClassifyError` (`internal/kube/classify.go`) sorts any transport/apiserver error into a 9-class taxonomy (`connection_refused | tls_cert | exec_plugin_missing | auth_expired | forbidden | dns | timeout | apiserver_5xx | unknown`) with per-class remediation (loopback-vs-remote tailored) + ADR-0004 doc link; `writeEngineError` now maps classes to faithful statuses/codes (401 auth_expired, 403, 409/422 read parity, 504 timeout, 502 apiserver_5xx/class, 502 cluster_unreachable only for unknown) and every call site + `ContextHealth` (new `reason`/`docURL`) + the error envelope (new `docURL`) + `ErrorState`/`errorTitle` carry it; `writeUnreachable`/`execGuidanceFor` deleted. **Story C (ADR-0007):** runtime kubeconfig source — `PUT /api/v1/kubeconfig` (path-only, validate-before-swap, opt-in via new canonical env `KUBESCOPE_ALLOW_KUBECONFIG_SET`, default off → 403 `kubeconfig_set_disabled`; inside the read-only guarded group so RO 403s it; success resets client cache + active override, returns fresh setup state); `Manager.SetKubeconfigPath/KubeconfigPath/ProbeContext/ClassifyActiveError` added thread-safe. **Story D:** watch-error handler now classifies + broadcasts a typed `status` SSE event once per transition (dampened repeats), a per-informer recovery prober retries a Limit-1 LIST on bounded exponential backoff (1s→30s cap), and recovery emits `connected` + resync; **recreated-cluster + silent-death cases covered** — health/setup probes feed the hub (`Hub.SyncContextHealth` via `HealthObserver`) so a failed probe marks informers unreachable even when a half-open TCP watch never errors; a successful probe evicts a cached client whose endpoint moved (`evictStaleClient`); and the prober probes with a freshly resolved client and rebuilds the informer in place when the endpoint changed (kind delete+create lands on a new port), so live views auto-resume without restart; FE shows the `ClusterBanner`, backs off polls while unreachable (health 30→60s, portforwards 5→30s, stream-fallback poll exponential via `fetchFailureCount`), and logs pre-open errors are classified too. **Story E (FB-7):** `testenv.sh run --docker` (+ `make testenv-run`/`testenv-run-docker`) — per-OS kubeconfig derivation preserving both contexts (macOS/Win host.docker.internal + TLS-skip rewrite; Linux --network host), image-presence guard/--build, trap-cleaned temp credentials; README/testenv-README document the caveats. Verified: `make test` (race+envtest) green incl. new classify/setup/set-kubeconfig/connectivity/rebuild tests; `fe-test` 199 (172+27); lint clean; binary smoke against kind — no-kubeconfig starter page → set path from UI → live overview; flag-off 403 + read-only 403; `kind delete` while viewing → banner + classified starter card + healthy-context switch works; recreate (new port) → informer rebuilt, auto-resume without reload. Two review fixes applied post-implementation (prober restart race; stale-client eviction/informer rebuild for moved endpoints). **Auth (8.1):** `KUBESCOPE_AUTH_MODE=basic` gates every route except `/healthz` with HTTP Basic auth (`authGuard`, `internal/server/auth.go`); credentials come from two new env vars `KUBESCOPE_AUTH_BASIC_USERNAME`/`KUBESCOPE_AUTH_BASIC_PASSWORD` (single operator/password — v1 model, recorded in ADR-0005 + CLAUDE.md canonical set). Constant-time compare over SHA-256 digests; credentials never logged (a failed attempt logs path/remote/had_credentials only). `none` passes through; `basic` with missing creds and `oidc` both **fail fast at startup** in `config.Load`. `/healthz` unauthenticated in all modes. The browser's native Basic challenge drives login, so the SPA needs no change. **Security pass (8.2):** read-only regression is now two-layer — the enumerated per-route 403 test plus a new `chi.Walk` route-surface test (`routes_test.go`) that fails if any mutating-method route is registered outside the guard/exempt classification; secret-handling audit swept every data path (get/yaml mask via `maskIfSecret`, list rows carry only counts, stream via `MaskStreamObject`, reveal is the sole value path and never logs) — no unmasked leak outside reveal. **FB-3 fixed:** a Host-header allowlist (`hostGuard`, `hostcheck.go`) sits ahead of every route — loopback names + the concrete bind host (with/without port), `/healthz` exempt; wildcard binds (0.0.0.0 — Docker default) pass through and rely on auth + network controls (closes the DNS-rebinding gap on the localhost-writable case). **CI (8.3):** `.github/workflows/ci.yml` (PR + push-to-main: `make lint`/`test`/`fe-test`/`build`) and `release.yml` (tag `v*` → buildx multi-arch amd64+arm64 image → `ghcr.io/skriptvalley/kubescope` with `{{version}}`, `{{major}}.{{minor}}`, `latest`). **Docs (8.4):** README rewritten (exposure warning, auth section, full env-var table, ADR-0004 cluster gotchas — kind/minikube networking, exec-plugin EKS/GKE, file-path certs); ADR-0005 addendum documents the basic-auth credential source + Host-allowlist; `CHANGELOG.md` summarizes sprints 0–8. Verified: `make test` (race + envtest) green incl. new config/auth/host-guard/route-surface tests; `fe-test` 172; gofmt/vet/eslint/tsc clean; `make build` embeds SPA (41MB binary); **binary smoke** — basic-auth 401→200 matrix, `/healthz` exempt, WWW-Authenticate challenge, rebinding Host → 403 forbidden_host, password absent from logs, read-only cordon → 403. Not yet done: v0.1.0 tag/release/publish (gated) and smoke against the published image (needs the release); Helm chart skipped → FB-4.
- Next expected: Cut v0.1.0 release (push `v0.1.0` tag → release workflow publishes the image → GitHub release + smoke the published image via the canonical docker run) — pending go-ahead; then v2 backlog.
- ADRs touched this session: 0007 (new — runtime kubeconfig source: path-only, opt-in via `KUBESCOPE_ALLOW_KUBECONFIG_SET`, recorded in the CLAUDE.md canonical env set). No new dependency. (Previous session: 0005 updated — Sprint 8 addendum: basic-auth credential source and the Host-allowlist/DNS-rebinding decision.)

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
- [x] FB-7: Dev-tooling — `deploy/testenv/testenv.sh` only has a bare-binary `run`; add a `run --docker` (and/or `make testenv-run-docker`) that rewrites the isolated testenv kubeconfig for a container (127.0.0.1→host.docker.internal, insecure-skip-tls-verify, both contexts, read-only mount) and runs the image. Captured as Story E in `prompts/feedback-cluster-connectivity-and-onboarding.md`. (source: user, priority: lo) [done — `run --docker` + `make testenv-run`/`testenv-run-docker`; per-OS derivation, trap-cleaned temp copy]
- [x] FB-6: Cluster-connectivity resilience & onboarding — never dead-end on a cluster problem. No-kubeconfig/no-context/unreachable should render a guided starter page (not a raw red error); classify apiserver/transport failures into a taxonomy with inline fix suggestions (absorbs FB-1); let users set/reload the kubeconfig from the UI (gated, needs an ADR — powerful capability under ADR-0004/0005); and handle a cluster torn down while viewing over SSE (typed unreachable event + bounded backoff + auto-resume, no busy-loop). Full plan: `prompts/feedback-cluster-connectivity-and-onboarding.md`. (source: user/manual testing, priority: hi) [done — setup-state starter page, `kube.ClassifyError` taxonomy end to end, ADR-0007 runtime kubeconfig source, typed SSE status + prober/backoff + informer rebuild + probe→hub sync; kind smoke of every scenario-matrix row that a host binary can hit]
- [ ] FB-5: OIDC auth mode (`KUBESCOPE_AUTH_MODE=oidc`) — stretch, not implemented in v1; currently fails fast at startup. Land alongside multi-user/hashed (htpasswd/bcrypt) credentials as the richer auth story; in-container exec-plugin/SSO login relates to FB-6 Story C. (source: sprint-8, priority: lo)
- [ ] FB-4: Minimal Helm chart for in-cluster deployment — deferred. An in-cluster Helm install implies a ServiceAccount-based product shape, which ADR-0004 explicitly rejects for v1 (Kubescope is built around a mounted host kubeconfig + context switching). Revisit if in-cluster deployment becomes a goal; a chart that mounts a kubeconfig from a Secret is the plausible bridge. (source: sprint-8, priority: lo)
- [x] FB-3: No Host-header allowlist on the API, so a DNS-rebinding page could reach a writable localhost instance (exec, mutations) despite the WebSocket Origin check — Origin-only checks can't stop rebinding, since the same-origin branch trusts `Host==Origin`. App-wide (every endpoint), not exec-specific; current mitigations are the loopback default bind, `KUBESCOPE_READ_ONLY`, and (soon) auth. Fix in the Sprint 8 security pass: a Host-allowlist middleware (configured listen addr + localhost/127.0.0.1[:port]) ahead of the API tree. (source: sprint-6 review, priority: med) [done — `hostGuard` in `internal/server/hostcheck.go`; loopback + concrete-bind allowlist, `/healthz` exempt, wildcard binds pass through to auth+network controls; covered by `hostcheck_test.go` + binary smoke]
- [x] FB-2: Context switch left the currently-mounted view (e.g. Overview) showing the prior cluster's data until a manual refresh or navigation. `useSwitchContext` removed cluster-scoped queries before the global invalidate, and a removed active query has no observer to refetch. Fixed: invalidate first (refetches mounted views in place), then drop only `type:"inactive"` caches. Regression test added; verified in-browser against two kind clusters. (source: manual testing, priority: hi) [done]
- [x] FB-1: `writeEngineError` collapses every non-NotFound/Forbidden apiserver error to `502 cluster_unreachable` (+ADR-0004 guidance); a genuine apiserver 5xx/conflict is then mislabeled. Sprint 5 addressed this on the write paths — `writeMutationError` classifies Conflict→409, Invalid/BadRequest→422 and AlreadyExists→409 before delegating, so apply/scale/delete/cordon/drain surface those faithfully. The generic *read* engine still routes through `writeEngineError` (reads rarely conflict); revisit only if a read path needs finer 5xx taxonomy. (source: sprint-2 review, priority: lo) [done — absorbed into FB-6 Story B: reads now classify Conflict/Invalid/401/403/timeout/5xx/transport faithfully via the taxonomy]
