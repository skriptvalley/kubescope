# Sprint 1 — Kubeconfig & context management + cluster overview

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching (kubeconfig/context manager in `internal/kube`).
3. ADRs: `docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md` (mounted read-only kubeconfig, embedded vs file-path creds, exec-plugin and local-cluster gotchas — the whole sprint leans on it).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
First-class multi-context support: switch between ≥2 contexts and see a per-cluster overview.

## Stories
### Story 1.1 — Kubeconfig parsing & context enumeration API
Parse the mounted kubeconfig fully (Sprint 0 only read the current context) and expose all contexts over the API.
**Acceptance criteria:**
- [ ] `GET /api/v1/contexts` returns every context: name, cluster, default namespace, and which one is active.
- [ ] Embedded `certificate-authority-data`/tokens work as-is; file-path cert references resolve when those files are also mounted (ADR-0004).
- [ ] Malformed or missing kubeconfig yields a structured error; the server stays up.
- [ ] Kubeconfig is treated strictly read-only — no writes to the mounted file, ever.

### Story 1.2 — Context switching + per-context rest.Config/client cache
Switch the active context; build and cache `rest.Config` + clients per context in `internal/kube`.
**Acceptance criteria:**
- [ ] A switch endpoint sets the active context; subsequent API calls target the new cluster.
- [ ] `rest.Config` + clientset are built lazily per context and cached — never rebuilt per request.
- [ ] The cache is concurrency-safe (`go test -race` clean).
- [ ] Context switch in the UI invalidates TanStack Query caches so all views refetch against the new cluster.

### Story 1.3 — Per-context connection/health status
Probe each context for: reachable, auth OK, server version.
**Acceptance criteria:**
- [ ] A health probe per context reports reachable / auth OK / server version via the API.
- [ ] Probes run concurrently with timeouts; one unreachable context never blocks the others or the UI.
- [ ] exec-plugin auth failures (EKS `aws eks get-token`, GKE plugin) surface the ADR-0004 guidance (mount `~/.aws`, bundle CLI, or pre-generate a token) instead of a raw error.
- [ ] The context list UI shows per-context status with the failure reason on error — no infinite spinners.

### Story 1.4 — Cluster overview page
Per-cluster overview for the active context, plus a context switcher in the UI chrome.
**Acceptance criteria:**
- [ ] Overview shows server version, node count, and namespace list for the active context.
- [ ] Context switcher (header or sidebar) lists contexts with their health status and switches on click.
- [ ] Overview refetches automatically on context switch.
- [ ] Unreachable-cluster errors render a clear error state, not a blank page.

## Task checklist
- [ ] `internal/kube`: full kubeconfig parser — contexts, clusters, users, active context.
- [ ] Credential resolution: embedded data vs file-path refs; map exec-plugin failures to ADR-0004 guidance text.
- [ ] `GET /api/v1/contexts` + context-switch endpoint in `internal/server`.
- [ ] Per-context `rest.Config`/clientset cache: lazy build, RWMutex-protected, race-tested.
- [ ] Health probe per context (version call with timeout, run concurrently) + status API.
- [ ] Overview API: server version, node count, namespace list for the active context.
- [ ] FE: typed API client calls for contexts, switch, health, overview.
- [ ] FE: context switcher component with status badges.
- [ ] FE: cluster overview page; TanStack Query invalidation on context switch.
- [ ] Manual smoke: two kind clusters merged into one kubeconfig; switch and verify the overview changes.

## Testing requirements
| Layer | Must cover |
|---|---|
| Table-driven Go tests | Kubeconfig parsing fixtures: multiple contexts, embedded creds, file-path refs, exec-auth entries, malformed files; exec-failure → guidance mapping |
| envtest | Contexts, switch, health, and overview endpoints against a fake apiserver (API-touching); `go test -race` on the client cache |
| vitest | Context switcher state + status badges; query-invalidation-on-switch; overview error state |
| Manual kind smoke | ≥2 contexts in one mounted kubeconfig; switch between them and confirm overview + node/namespace data changes per cluster |

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-1/<story-slug>`, open PR.
4. Agent code review on the PR diff; fix real findings on the branch (or log them as FB-N).
5. When gates are green (`make test` + `make lint` + `make fe-test`; green CI once Sprint 8 lands): squash-merge with a Conventional subject, delete the branch, sync local `main` (`git checkout main && git pull --prune`).
6. Print a concise summary: outcome + blockers only. The session ends with the work merged and the repo clean on up-to-date `main`.
