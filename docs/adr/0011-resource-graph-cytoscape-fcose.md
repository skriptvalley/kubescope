# 0011. Resource relationship graph: Cytoscape.js + fcose, namespace-scoped and focus-bounded

- **Status:** Accepted
- **Date:** 2026-07-26

## Context

FB-14 asks for a visual topology of a namespace's resources: ownership chains
(Deployment→ReplicaSet→Pod, CronJob→Job→Pod), traffic (Ingress→Service→Pod),
configuration (Pod/Job→ConfigMap/Secret), storage (Pod→PVC→PV), identity
(Pod→ServiceAccount) and scaling (HPA→target). Three forces shape the decision:

- **Compound nodes are the requirement, not a nicety.** The picture that makes a
  workload legible is a *box* — the Deployment's ReplicaSet, its pods and the
  Service fronting them drawn inside one circle, with shared config outside it.
  A graph library that cannot lay out nested nodes cannot draw this.
- **A namespace is not a diagram.** A whole-cluster (or even whole-namespace)
  dump is unreadable and unbounded; a per-minute CronJob alone accumulates
  near-identical runs forever.
- **Offline binary (0002).** The FE is embedded and runs with no network, so any
  library and any asset it needs must be bundled by Vite. No CDN, ever.

Adding a frontend graph library is a new dependency and a locked-decision change
(`CLAUDE.md` rule #3), so it needs an ADR.

## Decision

**Renderer — Cytoscape.js + the fcose layout.** `cytoscape` draws the graph and
`cytoscape-fcose` lays it out. fcose is the force-directed layout with
first-class **compound (nested) node** support, which is exactly the
Deployment-box requirement. Both are bundled by Vite; nothing is fetched at
runtime (0002). `cytoscape-fcose` ships no TypeScript types, so a one-line
ambient module declaration lives in `web/src/types/cytoscape-fcose.d.ts` rather
than an `any` at the call site.

**The graph is server-assembled.** A new `internal/graph` package walks the
cluster and returns a typed `{nodes, edges, groups}` DTO; the frontend maps that
DTO to Cytoscape elements and renders it. This keeps the frontend thin
(`CLAUDE.md`) and means every bound, every aggregation and every relation rule
is testable in Go.

**Discovery + dynamic client, like the rest of the engine (0003).** Every read
goes through the dynamic client against GVRs resolved from the shared discovery
cache. Consequences: a CRD can be the focus, and a custom controller that owns
core objects through `ownerReferences` traverses with no per-kind knowledge at
all. Kind resolution prefers the built-in group, so an operator shipping its own
`Deployment` cannot hijack an unqualified focus.

**Bounded on three axes.** Namespace-scoped, one focus object, and a small depth
(`?depth=`, default **3**, maximum **6**). Depth 3 from a Deployment reaches
ReplicaSet → pods → (Service, ConfigMap, Secret, ServiceAccount, PVC) — the
whole "what is this workload made of" picture, and no more. Two further caps
protect the render: **150 nodes** total and **24 children per node**. A
namespace with more than **500** objects of one kind truncates that list.

**Nothing is dropped silently.** Whenever a bound bites — node cap, fan-out cap,
truncated list, clamped depth, or a relation type that could not be listed — the
response sets `partial: true` and appends a human-readable `note` saying which,
and the view renders those notes in a banner. This mirrors the FB-11 counts
endpoint, which marks a paginated floor partial rather than reporting it as
exact.

**Clubbing — a rule about run series, not about size.** A *series of runs* —
a Job's pods (attempts) and a CronJob's Jobs (scheduled runs) — collapses into
one aggregated node from **two** upwards, carrying the count and a tally of the
outcomes ("3 Completed, 1 Failed"), so a clubbed failure still reads as a
failure. A controller's *replicas* are peers, not runs, and never club on those
grounds: `Deployment → 3 pods` stays three circles, which is the point of the
picture. Anything past the fan-out cap clubs regardless of kind, and that one is
dropped detail, so it also sets `partial`.

**Compound groups are synthetic.** `groups` are explicit parent boxes; nodes
point at theirs through `parent`. A group forms around a *root* — a pod
controller that nothing in the graph owns — and claims that controller, its
ReplicaSets, its pods (or their aggregate) and the Services fronting them. A
synthetic box rather than making the Deployment node itself the Cytoscape parent:
a compound parent's size and styling are driven by its children, so a Deployment
serving both roles would lose its own node rendering and status badge. Each node
joins the first box that claims it, which resolves the one genuinely ambiguous
case (a Service fronting two workloads can only sit in one box).

**Service⇄Pod comes from EndpointSlice, with two fallbacks.** EndpointSlice is
the source of truth where it is served; the v1 `Endpoints` object is the fallback
for older clusters — the same object `internal/resources/services.go` reads for
the typed Service detail, so the two views agree on which pods back a Service. A
Service with no endpoints at all falls back to matching its `spec.selector`
against the namespace's pods, labelled `selector` on the edge so an intended
membership is never passed off as a live one.

**Errors reuse the engine taxonomy.** A malformed focus is `400 invalid_focus`, a
bad depth `400 invalid_depth`, a kind the cluster does not serve
`404 unknown_resource`, a cluster-scoped focus `400 invalid_scope`; everything
cluster-side goes through `writeEngineError`, so a missing object is a
classified `404` and an unreachable apiserver is a classified `502` with
remediation — never a bare 500. A depth above the maximum is *clamped*, not
rejected: the response echoes the depth actually used and notes the clamp.

**Tone stays on the client.** Nodes carry a status *string*; the view maps it to
a Dusk tone through the app's one status→tone classifier (0009). A second
mapping in Go would drift from it. The only rule the graph adds is for
"ready/desired" ratios, which nothing else in the app renders.

**Read-only mode does not gate it.** The graph is a read.

## Consequences

**Positive:**
- The compound requirement is met by the library rather than hand-rolled.
- The traversal is dynamic-client-based, so CRDs and custom controllers work
  without being enumerated anywhere.
- Every bound is explicit, tested, and reported — a partial graph says so.
- Clubbing keys off *what a relation means* (runs vs replicas), so it stays
  readable without hiding the thing the reader came for.
- One namespace-wide list per kind per request (cached in a per-request store),
  so a graph costs a bounded, predictable number of API calls.

**Negative:**
- **Bundle weight.** `cytoscape` + `cytoscape-fcose` add ~577 KB raw / ~179 KB
  gzipped. Mitigated by code-splitting: the whole graph view is a `React.lazy`
  import, so it is a separate chunk and first paint does not pay for it. The main
  bundle is unchanged.
- Cytoscape renders to `<canvas>`, which cannot read CSS variables — the Dusk
  palette is resolved from the document and the stylesheet is rebuilt when the
  theme flips. Canvas `oklch()` support is required (every browser the app already
  targets has it, since the whole token layer is OKLCH).
- `cytoscape-fcose` has no upstream types; we carry a small declaration.
- Reverse ownership has no server-side index, so finding a controller's children
  means listing candidate kinds and filtering by `ownerReference` UID. The
  owner→child pairs are therefore an explicit table rather than a sweep. A
  custom controller's children are found through the *child's* ownerReferences
  when the child is reached some other way, but the graph will not discover an
  unknown kind's children by listing (follow-up: FB-18).
- Endpoints are read once per request, so the graph is a snapshot; it does not
  live-update over SSE. Refetching is a depth change or a remount (follow-up:
  FB-17).

## Alternatives considered

- **React Flow + elkjs** — rejected. React Flow's sub-flows are a manual
  construction (parent nodes must be positioned and sized by hand, and the
  layout engine has to be told about them), so the one requirement that drove
  the choice would have been the thing we implemented ourselves. Cytoscape
  treats compound nodes as a first-class concept and fcose lays them out
  directly. (Decided with the owner before the sprint.)
- **A whole-namespace or whole-cluster graph** — rejected. Unbounded and
  unreadable; the focus + depth model is what makes the picture answer a
  question.
- **Deriving Service→Pod from the selector only** — rejected as the primary
  path. The selector says what a Service *intends* to match; Endpoints/
  EndpointSlice says what it *currently* backs, including readiness, and is the
  same source the Service detail view already shows. The selector is the
  fallback for a Service with no endpoints yet.
- **Making the Deployment node the Cytoscape compound parent** — rejected. A
  parent's geometry is driven by its children, so the controller would lose its
  own node shape and status badge. A synthetic box keeps both.
- **Emitting a tone per node from Go** — rejected. FB-11/0009 centralized the
  status→tone rule on the client; a second copy would drift.
- **Live-updating the graph over SSE (0006)** — deferred to FB-17. A watch per
  kind per namespace for a view that is opened briefly is a poor trade; a
  snapshot with an explicit refetch is enough for v1.
