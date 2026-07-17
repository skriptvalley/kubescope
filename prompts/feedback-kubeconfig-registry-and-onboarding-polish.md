# Feedback — Kubeconfig source registry (multi-path/dir) & onboarding polish

> Feedback-driven prompt (FB-8 + FB-9), not part of the canonical Sprint 0–8 plan.
> Treat as a self-contained mini-sprint. Branch `fix/kubeconfig-registry`.
> One session; do not pull unrelated work.

## Context recap
Read before starting (in order):
1. `STATUS.md` — FB-8/FB-9; FB-6 landed the setup state, taxonomy, starter page, and the runtime single-path source (ADR-0007).
2. ADRs: `0004` (kubeconfig in Docker), `0005` (security posture), `0007` (runtime kubeconfig source — this mini-sprint **supersedes or extends** it; write the ADR first).
3. `internal/kube/manager.go` — the Manager already has: mutex-guarded source path, `SetKubeconfigPath` (validate-before-swap), **`SourceGeneration`** (bumped per swap; folded into the discovery cache key and stream informer keys, closes SSE streams on change) and a **source observer** (tears down exec/port-forwards). Reuse all of it — any registry mutation bumps the generation and fires the observer; do NOT invent a second invalidation path.

## Why (the trigger)
Users keep kubeconfigs as many files (one per cluster/team), not one merged file, and want to add a cluster to a *running* Kubescope from the UI. Analysis (recorded here so the constraint is never re-litigated):
- The UI path control resolves paths on the **process's** filesystem. Bare binary: any host path works today. Docker: only paths under mounts made at container creation are visible — runtime mounting does not exist, and every workaround (Docker socket, privileged host agent) is root-equivalent on the host: rejected under ADR-0005.
- Kubescope can never copy a host file it cannot read, and must never write into the read-only kubeconfig mount (ADR-0004).
- The feasible shape: mount a **directory** once (`-v ~/.kube:/kubeconfigs:ro`) — bind-mounted directories reflect files added on the host **live** — plus a source registry that merges multiple files and re-scans directories. Pasted/uploaded content remains the only mechanism for never-mounted files; it was rejected in ADR-0007 (secret surface, restart-lost source) and the new ADR must revisit and re-decide it explicitly (expected: still deferred).

## Goal
Kubescope reads an ordered **registry of kubeconfig sources** (files and directories), merged with kubectl semantics; users add/remove sources from the UI (starter page + a menu surface) within the mounted filesystem; dropping a file into a mounted directory registers it without a restart; and no raw kubeconfig error leaks past the starter page (FB-9).

## Stories

### Story A — Multi-source config + merged loading
**Acceptance criteria:**
- [ ] `KUBESCOPE_KUBECONFIG` accepts a colon-separated list; each entry is a file **or a directory**. No new env var. Single-path behavior is unchanged (zero regression for every existing run command).
- [ ] `kube.Manager` loads via `clientcmd` loading-rules **Precedence** (kubectl merge semantics: first occurrence of a context/cluster/user name wins). Directories expand (non-recursive, lexicographic) to their parseable kubeconfig files; unparseable/oversized (>1MiB)/hidden files are skipped with a per-file classified status, never a global failure.
- [ ] Contexts/health/overview/streams work across the merged set exactly as today; `SourceGeneration` semantics preserved.
- [ ] Shadowed names (same context in two sources) are detectable: surfaced per source (Story B's listing), not silently swallowed.

### Story B — Source registry API + UI (supersedes the single PUT)
**Acceptance criteria:**
- [ ] Registry endpoints replace `PUT /api/v1/kubeconfig` (pre-v0.1.0 — no compat burden): `GET /api/v1/kubeconfigs` (sources in precedence order: path, kind file|dir, per-source status: ok | missing | unparseable | empty, contexts contributed, shadowed names), `POST /api/v1/kubeconfigs` `{"path"}` (append), `DELETE` by path/id (remove). Gated exactly as before: `KUBESCOPE_ALLOW_KUBECONFIG_SET`, inside the read-only group, mutating-route tests updated.
- [ ] Runtime changes are an in-memory overlay over the env baseline (ADR-0007 semantics: restart reverts). Validate-before-apply: a bad add returns a classified error (invisible path ⇒ guidance that names the mounted-directory workflow) and leaves the registry untouched. Every successful mutation bumps `SourceGeneration` + fires the source observer.
- [ ] Removing the source that provides the active context degrades gracefully: active resolution falls back (remaining sources' current-context, else setup state `no_active_context` — never a crash or a stuck view).
- [ ] UI: the starter control becomes "Add a kubeconfig" against the registry; a "Manage kubeconfig sources" surface is reachable from the context-switcher menu (list + per-source status + add/remove, same flag/read-only gating as `canSetKubeconfig`).
- [ ] Setup state reflects the registry (`no_kubeconfig` only when NO source yields a usable file; `kubeconfigPath` field becomes the source summary — adjust the FE type).

### Story C — Directory re-scan (the runtime-add path)
**Acceptance criteria:**
- [ ] A file dropped into a registered directory on the host appears without restart: directory expansion happens at load time (the Manager already re-reads per request — keep that property; no background watcher, no caching of scan failures).
- [ ] Manual affordance: the sources UI has a Rescan/refresh action (may simply refetch — the per-request property makes it trivial) and the README documents the workflow: mount `~/.kube` (or a dedicated dir) once → drop files in → they appear.
- [ ] Removing a file from the directory behaves like removing a source (Story B's graceful degradation).

### Story D — ADR: kubeconfig source registry
**Acceptance criteria:**
- [ ] New ADR (0008) extending/superseding 0007: registry model (files+dirs, kubectl merge precedence, in-memory overlay, restart-reverts), directory-scan rules, the Docker mount analysis from "Why" (runtime mounts impossible; socket/privileged workarounds rejected), and an explicit paste/upload decision (expected: still rejected/deferred, with reasoning). ADR index + STATUS + CLAUDE.md canonical-env note updated (no new env var expected).

### Story E — FB-9: no raw errors outside the starter (polish)
**Acceptance criteria:**
- [ ] When setup state ≠ `ready`, the sidebar shows a muted placeholder instead of the red "discovering resources: …" error, and the context switcher shows a neutral state instead of red "kubeconfig error" — the starter page owns the messaging (observed leak: screenshot in FB-9).
- [ ] With a reachable cluster, both surfaces behave exactly as today; genuine mid-session errors (setup `ready`, discovery fails) still show.

## Scenario matrix
| Scenario | Expected |
|---|---|
| Single file (every existing run) | Unchanged, zero regression |
| `KUBESCOPE_KUBECONFIG=a.yaml:b.yaml` | Merged contexts, kubectl precedence |
| Entry is a directory of N files | All parseable files registered; per-file status |
| Directory contains one broken file | Others load; broken file listed unparseable; no global failure |
| UI add of a visible path | 200, contexts appear, generation bumped |
| UI add of an invisible path (Docker) | Classified 4xx + mounted-dir guidance; registry untouched |
| Drop file into mounted dir at runtime | Appears on next load/rescan, no restart |
| Remove source providing the active context | Graceful fallback / `no_active_context` starter |
| Same context name in two sources | First wins; shadowing visible in source listing |
| Restart after runtime changes | Env baseline restored |
| No usable source anywhere | Starter `no_kubeconfig`; sidebar/switcher muted (Story E) |

## Testing requirements
- Unit: config list/dir parsing; Manager merged loading + precedence + dir-scan skip rules; registry add/remove (validate-before-apply, generation bump, observer fired, active-context fallback); endpoint gating (routes/read-only classification for the new mutating routes); setup-state resolution over the registry.
- FE: vitest for the sources list/add/remove component, starter integration, switcher menu entry, FB-9 suppression (sidebar + switcher) in non-ready states.
- Manual kind smoke: two kind clusters exported to **separate files** in one mounted dir → both contexts appear; drop a third file in at runtime → appears without restart; remove active source → graceful; UI add of an invisible path → guided error.

## Definition of Done
- Compiles/builds; `make test` + `make lint` + `make fe-test` green; manual kind smoke above.
- ADR-0008 written; ADR-0007 re-statused/extended; README onboarding + env docs updated.
- `STATUS.md` updated (FB-8, FB-9 done; ADR recorded).

## End-of-session actions
1. `make test`, `make lint`, `make fe-test`.
2. Update `STATUS.md` (last work `[feedback]`, next expected, checkboxes, ADR).
3. Commit (Conventional Commits), push, PR; agent-review the diff and fix findings.
4. Gates + CI green → squash-merge, sync `main`; concise summary.
