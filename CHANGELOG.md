# Changelog

All notable changes to Kubescope are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow
[Semantic Versioning](https://semver.org/).

## [0.1.0] — 2026-07-17

First tagged release: a self-hostable, single-container Kubernetes dashboard that
mounts your kubeconfig, switches contexts, and browses and operates on every
resource type — with live updates, logs, exec, guarded mutations, and a
safe-by-default security posture. Summary of what landed across sprints 0–8 plus
the post-sprint connectivity, kubeconfig-source, and onboarding hardening rounds.

### Added

- **Single-container packaging (Sprint 0).** One Go binary embedding the built
  React SPA (ADR-0002); multi-stage, multi-arch (amd64 + arm64) Dockerfile;
  `/healthz`; env-based config (`KUBESCOPE_*`).
- **Kubeconfig & contexts (Sprint 1).** Parse the mounted kubeconfig, enumerate
  contexts, switch contexts with a per-context client cache, per-context
  reachability/auth/version health, and a cluster overview page.
- **Generic resource engine (Sprint 2).** Discovery of all API groups/resources
  including CRDs (cached, refreshable); dynamic get/list for any GVK with
  cluster- vs namespace-scope handling; generic list UI (sidebar nav from
  discovery, namespace selector) and detail + raw-YAML views (ADR-0003).
- **Workload deep views (Sprint 3).** Typed summaries for Pods, Deployments,
  StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs; pod detail (containers,
  statuses, restarts, conditions, placement); controller rollout status and
  owned-pod lists; related events on detail views.
- **Live updates, logs, events (Sprint 4).** Informer-backed watch→SSE bridge
  with per-context fan-out and reconnect; live-updating lists/details in the UI;
  pod log streaming (follow, container select, previous, tail); cluster-wide and
  per-namespace events feed (ADR-0006).
- **Mutations with guardrails (Sprint 5).** YAML edit + apply (CodeMirror,
  conflict surfacing); scale, rollout-restart, delete with typed confirmation;
  node cordon/uncordon/drain; server-side `KUBESCOPE_READ_ONLY` enforcement; and
  Secret masking with reveal-on-click (ADR-0005).
- **Exec & port-forward (Sprint 6).** WebSocket exec bridge (coder/websocket ⇆
  SPDY) with an xterm.js terminal (container select, resize, reconnect);
  start/stop/list port-forwards.
- **Config, networking, RBAC, storage + polish (Sprint 7).** ConfigMaps/Secrets,
  Services (resolved endpoints → pod links) and Ingress, RBAC
  (Roles/ClusterRoles, Bindings, ServiceAccounts), and storage (PV/PVC/
  StorageClass) views with cross-links; server-side per-kind list-column
  enrichment; global search; shared empty/error states; keyboard navigation.
- **Optional authentication (Sprint 8).** `KUBESCOPE_AUTH_MODE=basic` gates every
  route except `/healthz` with HTTP Basic auth, credentialed via
  `KUBESCOPE_AUTH_BASIC_USERNAME` / `KUBESCOPE_AUTH_BASIC_PASSWORD` (constant-time
  comparison, never logged). `oidc` is reserved and fails fast at startup.
- **DNS-rebinding protection (Sprint 8, FB-3).** A Host-header allowlist ahead of
  every route protects loopback/concrete binds; wildcard binds rely on auth +
  network controls.
- **CI/CD (Sprint 8).** GitHub Actions PR pipeline (Go + frontend lint, unit
  tests, embedded-SPA binary build) and a tag-triggered workflow publishing the
  multi-arch image to `ghcr.io/skriptvalley/kubescope`.
- **Cluster-connectivity resilience & onboarding (FB-6, FB-1).** No-kubeconfig,
  no-context, and unreachable states render a guided setup starter instead of a
  raw error; apiserver/transport failures are classified into an actionable
  taxonomy (`kube.ClassifyError`) surfaced end to end (read paths included); a
  cluster torn down mid-view emits a typed SSE unreachable status with bounded
  backoff and auto-resume; the kubeconfig source can be set at runtime from the
  UI, gated by `KUBESCOPE_ALLOW_KUBECONFIG_SET` (ADR-0007, superseded by 0008).
- **Kubeconfig source registry (FB-8).** `KUBESCOPE_KUBECONFIG` (and the
  `$KUBECONFIG` fallback) accepts an OS-path list of kubeconfig files *and
  directories*, merged with kubectl precedence (clientcmd); the default
  `/kubeconfig` probe accepts a directory, so `-v ~/.kube:/kubeconfig:ro` works
  with zero env vars. A registry API (`GET`/`POST /api/v1/kubeconfigs`,
  `DELETE /api/v1/kubeconfigs/{id}`) and UI (starter + context-switcher dialog)
  add and remove sources at runtime as an in-memory overlay (restart reverts),
  gated by `KUBESCOPE_ALLOW_KUBECONFIG_SET`; dropping a file into a mounted
  directory registers a new cluster without a restart (ADR-0008).
- **Onboarding polish (FB-9).** With no usable cluster connection, the sidebar
  and context switcher show muted placeholders keyed off setup state instead of
  leaking raw red errors; genuine mid-session errors stay red.

### Security

- Safe-by-default posture (ADR-0005): bare binary binds `127.0.0.1:8080`; Secret
  values masked by default and never logged (including the kubectl
  `last-applied-configuration` annotation); read-only mode rejects all mutations
  server-side; a route-surface regression test asserts every mutating route is
  guarded; the security pass audited every Secret data path.

### Known limitations

- exec-plugin clusters (EKS/GKE) and file-path-cert kubeconfigs need documented
  extra steps inside the container (ADR-0004).
- OIDC auth, multi-user/hashed credentials, and an in-cluster Helm deployment are
  deferred to a later release.

[0.1.0]: https://github.com/skriptvalley/kubescope/releases/tag/v0.1.0
