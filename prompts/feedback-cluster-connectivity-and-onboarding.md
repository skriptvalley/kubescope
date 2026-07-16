# Feedback — Cluster connectivity resilience & onboarding

> Feedback-driven prompt (post-v0.1.0), not part of the canonical Sprint 0–8 plan.
> Treat as a self-contained mini-sprint. Branch `fix/cluster-connectivity` (or
> `sprint-9/<slug>` if promoted to a sprint). One session; do not pull unrelated work.

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state; FB-6 (this) and FB-1 (error over-collapsing, which Story B absorbs).
2. `docs/ARCHITECTURE.md` — kube manager, resources, stream layers.
3. ADRs: `0004` (kubeconfig/auth in Docker — the source of most failure modes here), `0005` (security posture; a new "set kubeconfig at runtime" capability must be gated), `0006` (SSE/watch — live resilience).
One session; rules in `CLAUDE.md` apply. New config or a runtime-mutable kubeconfig source is an architecture change → **write/extend an ADR** (do not add silently, golden rule 3).

## Why (the trigger)
Running the container against the local kind testenv, the UI dead-ended on raw red errors across three distinct situations that should each be handled gracefully:
- `dial tcp 127.0.0.1:60034: connection refused` — container networking (kind advertises `127.0.0.1`; inside a container that is the container itself; ADR-0004).
- `dial tcp 192.168.65.254:60034: connection refused` — the same cluster **torn down** while Kubescope kept polling it (observed: the container logged `/contexts/health` + `/portforwards` every ~5s against a cluster that no longer existed).
- First run with no/again-wrong kubeconfig — a hard error instead of guidance.

The backend already **stays up** without a reachable cluster (`config.Load` never requires a kubeconfig; `kube.Manager` doesn't cache clients, so a fixed/added kubeconfig is picked up on the next request). The gap is entirely in **how failures are surfaced and recovered** — and the inability to point Kubescope at a kubeconfig from the UI.

## Goal
Never dead-end on a cluster problem. Every first-run and failure state renders an actionable next step (a guided starter page or an inline remediation with a fix suggestion), live views survive a cluster disappearing and reappearing, and users can point Kubescope at a kubeconfig from the UI — all without weakening the ADR-0005 security posture.

## Stories

### Story A — Never dead-end: first-run / no-cluster starter page
Replace raw error surfaces with a guided experience when there is nothing usable to show.
**Acceptance criteria:**
- [ ] Backend exposes a machine-readable **setup state** (extend `GET /api/v1/config` or add `GET /api/v1/setup/state`): one of `no_kubeconfig | no_contexts | active_unreachable | ready`, with a reason + guidance string. The UI branches on this enum, never by parsing error text.
- [ ] When state ≠ `ready`, the UI renders a **starter page** (not the red overview error): what a kubeconfig is, how to mount/point one for the bare binary vs Docker, the ADR-0004 local-cluster/exec-plugin/file-path caveats, and (from Story C) a control to set the kubeconfig path.
- [ ] With a valid, reachable context the app behaves exactly as today (no regression).
- [ ] Distinguish "no kubeconfig at all" from "kubeconfig present but its active cluster is unreachable" — different copy and next steps.

### Story B — Failure taxonomy + inline remediation (absorbs FB-1)
Turn opaque errors into classified, actionable ones everywhere a cluster call can fail.
**Acceptance criteria:**
- [ ] A single classifier maps apiserver/transport errors to a stable taxonomy: `connection_refused` (local-cluster/container networking), `tls_cert` (x509/unknown-authority/SAN), `exec_plugin_missing` (EKS/GKE/kubelogin binary absent), `auth_expired` (401), `forbidden` (403/RBAC), `dns` (no such host), `timeout`, `apiserver_5xx`. Each carries a short remediation + doc link.
- [ ] Fixes FB-1: `writeEngineError` (`internal/resources/errors.go`) no longer collapses every non-NotFound/Forbidden error to `502 cluster_unreachable`; genuine 5xx/conflict/timeout are labeled faithfully (align with the Sprint-5 `writeMutationError` taxonomy).
- [ ] `kube.ContextHealth.Guidance` (already present, ADR-0004 exec hints via `ExecGuidance`) is populated from the taxonomy for every probed context; the UI shows reason + suggestion + link at the point of failure (overview, lists, detail), not a bare red string.
- [ ] Secret/credential values never appear in any classified message or log (ADR-0005).

### Story C — Configure the kubeconfig from the UI (runtime, gated)
Let a user set/change which kubeconfig Kubescope reads, safely.
**Acceptance criteria:**
- [ ] `kube.Manager` gains a **thread-safe, runtime-settable** kubeconfig source (today `kubeconfigPath` is fixed in `NewManager`); a new **gated** endpoint sets/reloads it and re-probes, and `/api/v1/contexts` + setup state refresh without a restart.
- [ ] **ADR required** — decide and document: (a) set **path** only (must be readable by the process; in Docker only a mounted path works — surface that), vs (b) also accept **pasted/uploaded** kubeconfig content held **in memory only, never written to disk**. Pick the v1 shape; record in a new ADR (extend ADR-0004/0005).
- [ ] **Security gating (non-negotiable):** pointing the server at an arbitrary path/credentials is a powerful capability. It must be **disabled under `KUBESCOPE_READ_ONLY`**, require auth when `KUBESCOPE_AUTH_MODE` is set, and be opt-in (a flag and/or path allowlist — decide in the ADR). Never persist provided secrets; never log kubeconfig contents; the existing mounted default stays read-only.
- [ ] Invalid input (missing file, unparseable kubeconfig, no contexts) returns a classified error (Story B) and leaves the previous source intact.

### Story D — Live resilience: cluster loss/return over SSE + probes
Survive a cluster disappearing and coming back without a manual reload.
**Acceptance criteria:**
- [ ] When the active cluster becomes unreachable, watch→SSE streams (`internal/stream`) and log streams detect it, **stop tight-looping**, and emit a typed `status`/`error` SSE event so the client shows an "unreachable" banner with retry instead of silently spinning (observed failure: the container kept polling a deleted cluster).
- [ ] Auto-recovery: when the cluster returns, streams reconnect with backoff and live views resume; health/overview polling **backs off while unreachable** and speeds back up on recovery.
- [ ] The context-health surface reflects the transition (reachable → unreachable → reachable); switching to a healthy context always works even while another is down.
- [ ] No busy-loop or unbounded reconnect storm against a dead endpoint (bounded backoff; covered by a test).

### Story E — Dev-tooling: testenv container runner ("yes do that")
Make running the image against the local testenv one command.
**Acceptance criteria:**
- [ ] Add `deploy/testenv/testenv.sh run --docker` (and/or `make testenv-run-docker`) that derives a container-friendly copy of `deploy/testenv/kubeconfig` (`127.0.0.1`→`host.docker.internal`, drop CA data + `insecure-skip-tls-verify: true`, **both** dev/prod contexts), mounts it **read-only**, and runs the image on `:8080` — mirroring the macOS branch of `deploy/run-local.sh` but preserving both contexts.
- [ ] Document the caveat: `host.docker.internal` reaches the kind API only while the cluster exists; a torn-down cluster shows Story A/D states. Note `--network host` is Linux-only (not macOS Docker Desktop).
- [ ] No secrets written outside a temp file that is cleaned up (mirror `run-local.sh`'s trap).

## Configuration & negative-path scenario matrix
Cover each; each row is a test + a defined UX (starter page vs inline remediation vs live banner):

| Scenario | Detection | Expected UX |
|---|---|---|
| No kubeconfig at path | `Manager` load error / setup state `no_kubeconfig` | Starter page: mount/point a kubeconfig (Story A/C) |
| Kubeconfig present, 0 contexts | setup state `no_contexts` | Starter page: add a context / fix file |
| Local cluster, container can't reach `127.0.0.1` | `connection_refused` | Inline: ADR-0004 networking fix (host.docker.internal / --network / port) |
| Active cluster torn down while viewing | probe flips unreachable; SSE watch errors | Live banner + backoff; auto-resume on return (Story D) |
| exec-plugin auth (EKS/GKE/kubelogin) missing in container | `exec_plugin_missing` | Inline: ADR-0004 exec-plugin options; per-context, others keep working |
| Token/SSO expired (401) | `auth_expired` | Inline: re-authenticate on host + refresh/repoint kubeconfig (Story C) |
| RBAC forbidden (403) | `forbidden` | Inline: insufficient permissions for this resource, not a global failure |
| TLS cert / missing SAN | `tls_cert` | Inline: CA mount / insecure-skip (local dev) guidance |

**Auth/SSO scope note:** Kubescope cannot run interactive SSO/exec-plugin logins itself (no host CLI/browser in the container — ADR-0004). Story B/C handling is **detect + classify + guide** (and let the user repoint to a refreshed kubeconfig/token), **not** performing the login. Implementing in-container exec-plugin/OIDC login stays a v2 item (relates to FB-5).

## Testing requirements
- Unit: error classifier table (each taxonomy class → code + guidance); setup-state resolution (no_kubeconfig / no_contexts / unreachable / ready); Manager runtime source switch (thread-safe, re-probe, invalid input leaves prior source intact); read-only/auth gating of the set-kubeconfig endpoint; SSE unreachable→typed-event and bounded-backoff reconnect.
- envtest where a real apiserver helps (probe transitions, RBAC 403).
- Manual kind smoke: bring the cluster up → ready; `kind delete cluster` while viewing → live banner + backoff, no busy loop; `kind create` again → auto-resume; start with no kubeconfig → starter page; set a kubeconfig path from the UI → contexts appear.
- FE: vitest for the starter page, the setup-state branch, and the inline-remediation component.

## Definition of Done
- Compiles/builds; lint clean; unit tests for new logic pass.
- Manual kind smoke of the loss/return + onboarding + set-kubeconfig flows.
- New ADR for the runtime kubeconfig-source capability (Story C); ADR-0004/0005 cross-linked; README onboarding + env docs updated.
- `STATUS.md` updated (FB-6 done; FB-1 folded into Story B; note any deferrals).

## End-of-session actions
1. Run `make test` and `make lint` (+ `make fe-test`).
2. Update `STATUS.md` (last work + type `[feedback]`, next expected, FB checkboxes; record the new ADR).
3. Commit (Conventional Commits), push branch, open PR; agent-review the diff and fix findings.
4. When gates + CI are green, squash-merge, sync `main`; print a concise summary (outcome + blockers only).
