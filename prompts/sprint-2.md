# Sprint 2 — Generic resource engine (read-only)

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching (generic resource engine in `internal/resources`).
3. ADRs: `docs/adr/0003-generic-resource-access-via-discovery-and-dynamic-client.md` (discovery + dynamic client is how ALL resources incl. CRDs are supported generically; typed hot-path handlers come in Sprint 3).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Browse ANY resource type in the active cluster read-only — including CRDs.

## Stories
### Story 2.1 — Discovery: enumerate all API groups/resources incl. CRDs (cached, refreshable)
Discovery service in `internal/resources`: enumerate every API group/version/resource the active cluster serves.
**Acceptance criteria:**
- [ ] A discovery endpoint lists all groups/versions/resources for the active context, CRDs included.
- [ ] Each entry carries GVR, kind, namespaced flag, and supported verbs.
- [ ] Results are cached per context with an explicit refresh; a CRD installed after startup appears on refresh without a restart.
- [ ] Partial discovery failures degrade gracefully: available groups are returned, the failure is surfaced.

### Story 2.2 — Dynamic client get/list; cluster- vs namespace-scoped handling; namespace selector API
Generic get/list for any GVR via the dynamic client, with correct scope handling and a namespace list API.
**Acceptance criteria:**
- [ ] A generic list endpoint serves any GVR via the dynamic client (parameterized by group/version/resource).
- [ ] Namespace-scoped resources accept a namespace or all-namespaces; cluster-scoped resources reject a namespace parameter.
- [ ] A namespace list API backs the UI selector.
- [ ] Get-single-object returns the full object, suitable for raw YAML rendering.
- [ ] Unknown GVR returns 404 with a structured error body.

### Story 2.3 — Generic resource list UI (TanStack Table, sidebar nav built from discovery, namespace selector)
One generic list page that works for every resource type, driven entirely by discovery data.
**Acceptance criteria:**
- [ ] Sidebar nav is generated from discovery data, grouped by API group — not hardcoded.
- [ ] TanStack Table v8 list with name / namespace / age columns and client-side sorting.
- [ ] Namespace selector (single namespace or all) drives namespaced lists; hidden for cluster-scoped kinds.
- [ ] CRD instances browse identically to core resources through the same page.

### Story 2.4 — Generic resource detail view + raw YAML tab
Generic detail page for any object plus a read-only YAML tab. Editing (CodeMirror) is Sprint 5; Secret masking is Sprint 5 — do not build either here.
**Acceptance criteria:**
- [ ] Detail view renders metadata generically: labels, annotations, ownerReferences, creation time/age.
- [ ] Raw YAML tab shows the full object read-only with syntax highlighting.
- [ ] Every object has a deep-linkable route (group/version/resource[/namespace]/name).
- [ ] Not-found and permission errors render structured error states.

## Task checklist
- [ ] `internal/resources`: discovery service — per-context cache, refresh, GVR metadata (namespaced flag, verbs).
- [ ] Discovery API endpoint shaped for sidebar-nav consumption.
- [ ] Dynamic-client list/get handlers with GVR-parameterized routes in `internal/server`.
- [ ] Cluster- vs namespace-scope routing + validation.
- [ ] Namespace list API with all-namespaces support.
- [ ] Structured 404/error responses for unknown GVRs and missing objects.
- [ ] FE: typed API client calls for discovery, generic list/get, namespaces.
- [ ] FE: sidebar nav generated from discovery, grouped by API group.
- [ ] FE: generic list page (TanStack Table, sorting, namespace selector).
- [ ] FE: generic detail page + read-only YAML tab, deep-linkable routes.
- [ ] Manual smoke on kind: install a sample CRD + custom resource, browse it end-to-end.

## Testing requirements
| Layer | Must cover |
|---|---|
| Table-driven Go tests | GVR route parsing, scope validation (namespaced vs cluster), error mapping (unknown GVR, missing object) |
| envtest | Discovery enumeration incl. a registered sample CRD; dynamic list/get for core kinds and the CRD; namespace scoping (API-touching) |
| vitest | Nav building from discovery payloads; table column/sort logic; namespace selector state; YAML tab rendering |
| Manual kind smoke | Browse several core kinds + one installed CRD read-only, including namespace switching and deep links |

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-2/<story-slug>`, open PR.
4. Agent code review on the PR diff; fix real findings on the branch (or log them as FB-N).
5. When gates are green (`make test` + `make lint` + `make fe-test`; green CI once Sprint 8 lands): squash-merge with a Conventional subject, delete the branch, sync local `main` (`git checkout main && git pull --prune`).
6. Print a concise summary: outcome + blockers only. The session ends with the work merged and the repo clean on up-to-date `main`.
