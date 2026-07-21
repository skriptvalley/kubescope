# v2 Sprint 2 — Resource relationship graph

> v2 sprint (implements **FB-14**; the v2-backlog "Resource graph"). Branch `sprint-v2-2/resource-graph`.
> One sprint per session. Independent of FB-13, but shares the testenv fixtures. Rules in `CLAUDE.md` apply.

## Context recap
Read before starting (in order):
1. `STATUS.md` — FB-14; v2 backlog "Resource graph". The testenv `web`/`batch` namespaces are the fixtures (Deployment + Service; CronJob → Jobs). **Depends on FB-12 Story A** for the Secret/ConfigMap→workload wiring: in today's testenv the `api-credentials` Secret and `frontend-config` ConfigMap are standalone objects that no workload references, so those edges only exist once FB-12 lands.
2. `docs/ARCHITECTURE.md`; `docs/adr/0003` (typed vs discovery+dynamic — the builder is discovery/dynamic-backed so it also covers CRDs); `0009` (Dusk design system — the graph view must use Dusk tokens). **This sprint writes a new ADR** for the graph library.
3. `internal/resources/` — reuse existing relationship computations rather than re-deriving them: `owned.go` (ownerReferences / owned-pod lists), `services.go` (Service → endpoint pods), `events.go`. The FB-11 counts endpoint (`countResource`, which returns a **partial** marker past the pagination cap) is the pattern for bounding.

## Sprint goal
A **namespace-scoped, focus + depth-N** resource relationship graph: a backend `internal/graph` package assembles a typed `{nodes, edges, groups}` DTO, and the frontend renders it with **Cytoscape.js + fcose** (compound nodes) themed to Dusk.

## Stories

### Story 2.1 — Backend graph builder (`internal/graph`)
From a focus resource in a namespace, walk relationships to depth N and emit a typed DTO. Discovery/dynamic-backed.
**Acceptance criteria:**
- [ ] Edges derived from: ownerReferences (Deployment→ReplicaSet→Pod, StatefulSet/DaemonSet→Pod, Job→Pod, CronJob→Job); Service→Pods (selector / EndpointSlice); Pod/Job→Secret & →ConfigMap (volumes, `envFrom`, `env.valueFrom`, `imagePullSecrets`); Pod→PVC→PV; Pod→ServiceAccount; Ingress→Service; HPA→scaleTargetRef.
- [ ] Nodes are typed (kind + name + namespace + minimal status); a **group/parent** relation expresses compound nesting (a Deployment as the parent circle of its Service + pods).
- [ ] Bounded: namespace-scoped, focus + depth-N; a node cap with a **partial** marker when exceeded (mirror `countResource`) — never a silent truncation or an unbounded cluster dump.

### Story 2.2 — Clubbing & aggregation
Keep the graph readable when a controller fans out.
**Acceptance criteria:**
- [ ] A Job/CronJob "series of runs" (many pods) collapses into one aggregated node carrying a run/pod count, not N pod nodes.
- [ ] Over-cap fan-outs are summarized into an aggregate node (with count) and the partial marker is set — dropped detail is surfaced, never silent.

### Story 2.3 — Graph API
**Acceptance criteria:**
- [ ] `GET /api/v1/namespaces/{namespace}/graph?focus=<kind>/<name>&depth=<N>` returns the DTO (small default depth, capped). Use the established `{namespace}` path param — consistent with the existing `/api/v1/namespaces/{namespace}/…` handlers (e.g. quotas, service detail in `internal/server/server.go`). A read — not gated by read-only mode.
- [ ] Missing focus / empty namespace / unreachable cluster return the standard **classified** errors (reuse the engine taxonomy), not a bare 500.

### Story 2.4 — Frontend graph view (Cytoscape.js + fcose)
**Acceptance criteria:**
- [ ] New FE deps `cytoscape` + `cytoscape-fcose`, **bundled/self-hosted** (no CDN — ADR-0002); DTO→elements mapping in the typed API layer, consumed via a TanStack Query hook (no `fetch` in components).
- [ ] Compound nodes render the Deployment circle containing its Service + pods; node/edge styles use **Dusk tokens** and distinguish kinds; empty / partial / error states handled.
- [ ] Reachable from a resource detail ("View graph" / focus action); a depth control; clicking a node navigates to that resource's detail.

### Story 2.5 — ADR: graph library
**Acceptance criteria:**
- [ ] New ADR: adopt **Cytoscape.js + fcose** (over React Flow + elkjs) for the compound-node requirement; record the ns-scoped + focus + depth-N bounding decision, the clubbing rule, the new-dep + bundle-size note, and the self-hosted/offline constraint (ADR-0002). ADR index + `STATUS.md` + the CLAUDE.md FE-stack note updated.

## Task checklist
- [ ] `internal/graph` builder + edge derivations (2.1) + tests.
- [ ] Clubbing/aggregation + cap/partial (2.2).
- [ ] Graph API handler (2.3).
- [ ] Cytoscape/fcose view + hook + Dusk styling (2.4).
- [ ] ADR + STATUS + CLAUDE.md stack note.

## Testing requirements
- Unit (Go): each edge-derivation type (table-driven); clubbing; depth bounding; node cap + partial marker; envtest over a seeded namespace asserting the expected node/edge set.
- FE (vitest): DTO→Cytoscape element mapping, compound grouping, empty/partial/error states, and the view rendering from a fixture DTO.
- Manual kind smoke (testenv, **after FB-12 Story A** wires the Secret/ConfigMap into a workload): focus the `web` `frontend` Deployment → a compound circle with its Service + 3 pods, plus the `api-credentials` Secret / `frontend-config` ConfigMap edge (from the FB-12 wiring); the `batch` namespace → CronJob run-clubbing into one aggregated node.

## Definition of Done
- Compiles/builds; `make test` + `make lint` + `make fe-test` green; manual kind smoke above; the production FE bundle greps clean of any CDN/external graph asset.
- New ADR written; `STATUS.md` + the CLAUDE.md FE-stack note updated (Cytoscape added).

## End-of-session actions
1. `make test`, `make lint`, `make fe-test` + the manual kind smoke; bundle grep.
2. Update `STATUS.md` (last work `[sprint]` — v2 Sprint 2 / FB-14, next expected, checkboxes, ADR).
3. Commit (Conventional Commits), push the branch, open a PR; agent-review the diff and fix findings.
4. Gates green → squash-merge, sync `main`; concise summary.
