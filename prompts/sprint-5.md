# Sprint 5 — Mutations + guardrails

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching.
3. ADRs: `docs/adr/0005-security-posture-read-only-and-secret-masking.md` (guardrails are the point of this sprint) and `docs/adr/0003-generic-resource-access-via-discovery-and-dynamic-client.md` (generic apply/delete go through the dynamic client).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Safe write operations: edit/apply, scale, rollout-restart, delete, and cordon/drain — every mutation behind a confirmation dialog, `KUBESCOPE_READ_ONLY=true` returning 403 from server-side middleware on all mutating endpoints (never UI-only enforcement), and Secret values masked by default.

## Stories
### Story 5.1 — Edit YAML + apply (CodeMirror editor, server-side update, conflict surfacing)
Editable YAML tab (CodeMirror 6) on the generic detail view; the server applies updates via the dynamic client so any GVK — including CRDs — is editable (ADR-0003).
**Acceptance criteria:**
- [ ] YAML tab has an edit mode with CodeMirror 6 YAML syntax highlighting.
- [ ] Update endpoint applies via the dynamic client generically; a CRD instance is editable with no kind-specific code.
- [ ] resourceVersion conflict (409) is surfaced with a reload-and-retry option — never silently overwritten.
- [ ] Invalid YAML and server-side validation errors are shown inline in the editor.
- [ ] Saving requires an explicit confirmation dialog.

### Story 5.2 — Scale, rollout-restart, delete — with typed confirmation dialogs
Action endpoints plus UI controls on workload views. Delete is generic (any GVK via dynamic client); scale targets Deployments/StatefulSets/ReplicaSets; rollout-restart patches the pod template restart annotation.
**Acceptance criteria:**
- [ ] Scale endpoint + replica control on Deployment/StatefulSet/ReplicaSet views.
- [ ] Rollout-restart triggers a new rollout, visible in Sprint 3 rollout status.
- [ ] Delete works for any GVK, namespaced or cluster-scoped, from list and detail views.
- [ ] Every action opens a confirmation dialog; delete requires typing the resource name to confirm.
- [ ] Action failures (RBAC denied, not found) are surfaced with the server error message.

### Story 5.3 — Node cordon/uncordon/drain
Node operations: cordon/uncordon patch `spec.unschedulable`; drain uses the eviction API, respects PodDisruptionBudgets, and skips DaemonSet pods.
**Acceptance criteria:**
- [ ] Cordon/uncordon toggles node schedulability and reflects immediately in the node view.
- [ ] Drain evicts via the eviction API, skips DaemonSet-owned pods, and reports per-pod progress.
- [ ] PDB-blocked or failed evictions are reported per pod, not swallowed.
- [ ] Drain requires typing the node name in the confirmation dialog.

### Story 5.4 — `KUBESCOPE_READ_ONLY` enforcement (server middleware + UI state) + Secret masking
The two hard guardrails from ADR-0005. Read-only is enforced by server-side middleware on every mutating route — the UI state is a convenience, not the control. Secret data is masked by default everywhere it renders.
**Acceptance criteria:**
- [ ] With `KUBESCOPE_READ_ONLY=true`, server middleware returns 403 on ALL mutating endpoints (apply, scale, rollout-restart, delete, cordon/uncordon, drain) — verified by a test that enumerates every mutating route.
- [ ] A direct API call (curl, bypassing the UI) is equally rejected — enforcement does not depend on the frontend.
- [ ] UI reads read-only state from the API and disables/hides all mutation controls with an explanatory notice.
- [ ] Secret data values are masked by default in list, detail, and YAML views; reveal is per-key, on click.
- [ ] Secret values never appear in server logs (slog audit of all mutation/read paths).

## Task checklist
- [ ] Implement generic update (apply) endpoint via dynamic client in `internal/resources`.
- [ ] Implement scale, rollout-restart, and generic delete endpoints.
- [ ] Implement cordon/uncordon patch + drain with eviction API, PDB handling, DaemonSet skip.
- [ ] Implement read-only middleware in `internal/server` gating every mutating route; wire `KUBESCOPE_READ_ONLY` through `internal/config`.
- [ ] Expose read-only state to the FE (e.g. via an existing config/context endpoint).
- [ ] Implement Secret masking server-side so raw values are not shipped unless explicitly revealed.
- [ ] FE: CodeMirror 6 YAML edit mode with conflict + validation error surfacing.
- [ ] FE: confirmation dialog component (typed-name variant for delete/drain) used by every mutation.
- [ ] FE: scale/rollout-restart/delete controls on workload views; cordon/drain on node view.
- [ ] FE: read-only mode — disable mutation controls + notice; masked Secret rendering with per-key reveal.
- [ ] Table-driven Go tests: read-only middleware over every mutating route; drain pod-selection logic; conflict handling.
- [ ] envtest: apply (incl. CRD instance + 409 conflict), scale, delete, cordon, drain eviction path.
- [ ] vitest: confirmation dialogs (typed-name gating), editor error states, read-only UI, Secret masking/reveal.
- [ ] Manual kind smoke (see Testing requirements).

## Testing requirements
| Layer | Must cover |
|---|---|
| Table-driven Go tests | Read-only middleware: every mutating route × read-only on/off → 403/pass-through; drain candidate selection (DaemonSet skip, PDB); rollout-restart patch shape |
| envtest | Generic apply on a CRD instance; 409 conflict on stale resourceVersion; scale subresource; delete; cordon patch; drain eviction incl. PDB-blocked case |
| vitest | Typed-name confirmation gating; YAML editor conflict/validation display; read-only disables all mutation controls; Secret values masked until per-key reveal |
| Manual kind smoke | Edit + apply a Deployment; scale up/down; rollout-restart; delete a scratch resource; cordon + drain a node; restart with `KUBESCOPE_READ_ONLY=true` and confirm curl mutations get 403 and the UI is read-only; view a Secret and confirm masking |

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-5/<story-slug>`, open PR.
4. Print a concise summary: outcome + blockers only.
