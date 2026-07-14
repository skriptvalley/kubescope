# Sprint 3 — Workload deep views

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching.
3. ADRs: `docs/adr/0003-generic-resource-access-via-discovery-and-dynamic-client.md` (typed handlers are the hot-path complement to the generic engine — do not replace it).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Rich, resource-appropriate deep views for workloads: typed backend summaries plus detail pages for Pods and their controllers, with related events.

## Stories
### Story 3.1 — Typed backend summaries for Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs
Add typed handlers in `internal/resources` alongside the Sprint 2 generic engine. Each kind gets a summary list endpoint (per context + namespace) with kind-appropriate row fields computed server-side. The dynamic-client path stays as the fallback for everything else (ADR-0003).
**Acceptance criteria:**
- [ ] Typed list endpoints exist for all seven kinds, scoped by context and namespace (or all namespaces).
- [ ] Summary fields computed in Go, not the FE: e.g. Pod ready-container count, restarts, phase, node; Deployment ready/updated/available replicas; CronJob schedule + last run; age for all kinds.
- [ ] Generic engine untouched and still serves these kinds if requested generically.
- [ ] Summary response shapes are typed in the FE API client (no `any`).

### Story 3.2 — Pod detail: containers, statuses, restarts, conditions, node placement
Dedicated Pod detail view layered on the generic detail page, replacing the plain object dump for Pods.
**Acceptance criteria:**
- [ ] Per-container section: image, state (waiting/running/terminated with reason), ready flag, restart count.
- [ ] Init and ephemeral containers rendered as distinct groups.
- [ ] Phase, conditions, node name, pod IP, QoS class, and owner (with link) displayed.
- [ ] Raw YAML tab from Sprint 2 remains available on the same page.

### Story 3.3 — Controller views: replicas, rollout status, owned-pod lists
Detail views for Deployments/StatefulSets/DaemonSets/ReplicaSets/Jobs/CronJobs showing replica health and the pods they own (resolved server-side via selector + ownerReferences).
**Acceptance criteria:**
- [ ] Deployment/StatefulSet/DaemonSet detail shows desired/ready/updated/available counts and a rollout status line.
- [ ] Owned-pods table on controller detail, each row linking to Pod detail.
- [ ] Job detail shows completions/succeeded/failed; CronJob detail shows schedule, suspend flag, last schedule time, active Jobs.
- [ ] ReplicaSet detail links to its owning Deployment when one exists.

### Story 3.4 — Related events on workload detail views
Backend endpoint for events filtered by involvedObject; shared FE events panel embedded in every workload detail view.
**Acceptance criteria:**
- [ ] Events endpoint filters by involvedObject kind + name + namespace.
- [ ] Panel shows type, reason, message, count, and last-seen age, sorted newest-first.
- [ ] Warning events visually distinct from Normal.
- [ ] Clean empty state when no events exist.

## Task checklist
- [ ] Define workload summary row types + endpoints for the seven kinds in `internal/resources`.
- [ ] Implement server-side summary computation (ready counts, restarts, rollout status, age).
- [ ] Add owned-pods resolution (label selector + ownerReferences) server-side.
- [ ] Add events-by-involvedObject endpoint.
- [ ] Register routes in `internal/server`; wire per-context clients from `internal/kube`.
- [ ] FE: extend typed API client with summary, owned-pods, and events calls.
- [ ] FE: workload list pages with kind-specific columns (TanStack Table).
- [ ] FE: Pod detail view (containers, conditions, placement).
- [ ] FE: controller detail views with replica status + owned-pods table.
- [ ] FE: reusable events panel component embedded in all workload detail views.
- [ ] Table-driven Go tests for every summary computation.
- [ ] envtest coverage for typed endpoints, owned-pods resolution, and event filtering.
- [ ] vitest coverage for Pod detail, controller detail, and events panel.
- [ ] Manual kind smoke (see Testing requirements).

## Testing requirements
| Layer | Must cover |
|---|---|
| Table-driven Go tests | Summary field computation per kind (ready counts, restarts, rollout status, CronJob last-run, age) incl. nil/edge status fields |
| envtest | Typed list endpoints return correct rows; owned-pods resolution via selector + ownerReferences; events filtered by involvedObject |
| vitest | Pod detail renders container states/restarts; controller detail renders replica counts + owned-pods links; events panel sorting + empty state |
| Manual kind smoke | Deploy a sample app (Deployment + CronJob + Job); verify all seven list views, Pod detail, controller drill-down to pods, and events on a crashing pod |

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-3/<story-slug>`, open PR.
4. Print a concise summary: outcome + blockers only.
