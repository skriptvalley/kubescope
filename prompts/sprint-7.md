# Sprint 7 — Config/networking/RBAC/storage + polish

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching.
3. ADRs: `docs/adr/0003-generic-resource-access-via-discovery-and-dynamic-client.md` (generic engine + typed hot paths), `docs/adr/0005-security-posture-read-only-and-secret-masking.md` (Secret masking rules).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Resource-appropriate views for config, networking, RBAC, and storage resources, plus global search and polished empty/error states.

## Stories

### Story 7.1 — ConfigMaps + Secrets (masked by default, reveal-on-click)
Dedicated views on top of the generic engine. Secrets follow ADR-0005: values masked everywhere by default, per-key reveal-on-click, never logged.
**Acceptance criteria:**
- [ ] ConfigMap detail lists data keys with values (long values collapsible).
- [ ] Secret detail lists keys with values masked; each key has reveal-on-click (decoded on demand, re-masks on navigation).
- [ ] Secret list/detail API payloads and the YAML tab omit or mask `data` values until explicitly revealed.
- [ ] Secret values never appear in server logs, including on error paths.

### Story 7.2 — Services + Ingress views
Typed columns and detail views for Services and Ingresses.
**Acceptance criteria:**
- [ ] Service list shows type, cluster IP, ports, selector; detail shows endpoints with ready/not-ready addresses.
- [ ] Ingress list shows hosts, paths, backends; detail renders rules and TLS config.
- [ ] Selector on a Service detail links to the matching pod list.
- [ ] Both views work in every namespace and update on refresh without errors.

### Story 7.3 — RBAC: Roles/ClusterRoles, Bindings, ServiceAccounts
Readable RBAC views: rule tables instead of raw YAML walls.
**Acceptance criteria:**
- [ ] Role/ClusterRole detail renders rules as a table (apiGroups / resources / verbs).
- [ ] RoleBinding/ClusterRoleBinding detail shows subjects and roleRef, with a link to the referenced role.
- [ ] ServiceAccount detail shows namespace, secrets, and image pull secrets.
- [ ] Cluster-scoped vs namespaced variants are correctly separated in nav and lists.

### Story 7.4 — Storage: PV, PVC, StorageClass
Storage views wired to each other.
**Acceptance criteria:**
- [ ] PVC list shows status, capacity, access modes, StorageClass, bound PV; detail links to the PV.
- [ ] PV detail shows reclaim policy, capacity, claim reference (linked), and phase.
- [ ] StorageClass list shows provisioner and marks the default class.
- [ ] Unbound/pending PVCs render a meaningful status, not an empty view.

### Story 7.5 — Global search + empty/error states + keyboard nav
Cross-resource name search within the current context, plus a polish pass on every list/detail view.
**Acceptance criteria:**
- [ ] Global search (`/` to focus) matches resource names across discovered types in the current context and navigates to the result.
- [ ] Every list has an explicit empty state; every list/detail has an error state with a retry action.
- [ ] Keyboard nav: `/` focuses search, `Esc` closes dialogs/panels, arrow keys move list selection; shortcuts documented in a help popover.
- [ ] Search degrades gracefully (partial results + notice) when a resource type fails to list.

## Task checklist
- [ ] Backend: Secret masking at the API layer — masked list/detail payloads + explicit per-key reveal endpoint honoring ADR-0005.
- [ ] Audit log paths touched this sprint to confirm no Secret values can be logged.
- [ ] Backend: typed summaries (columns) for Service, Ingress, endpoints resolution.
- [ ] Backend: typed summaries for Role/ClusterRole, bindings, ServiceAccount; rules flattening for the UI table.
- [ ] Backend: typed summaries for PV, PVC, StorageClass incl. bound-object cross-references.
- [ ] Backend: search endpoint — name matching across discovered types in the active context, bounded result set.
- [ ] Frontend: ConfigMap + Secret detail views with masked values and reveal-on-click.
- [ ] Frontend: Service/Ingress, RBAC, and storage detail views with cross-links (Service→pods, PVC⇆PV, binding→role).
- [ ] Frontend: global search UI (`/` shortcut, results dropdown, navigate-on-select).
- [ ] Frontend: shared empty-state and error-state components applied to all lists/details.
- [ ] Frontend: keyboard navigation + shortcuts help popover.
- [ ] Register all new views in sidebar nav (follow `.claude/skills/add-resource-view`).
- [ ] Cover everything in Testing requirements below.

## Testing requirements
- Unit: Secret masking (list/detail payloads masked, reveal path decodes exactly one key, nothing logged); RBAC rules flattening; PVC/PV cross-reference resolution; search matching + result bounding.
- envtest: Secret endpoints against a fake apiserver — masked by default, reveal returns decoded value; Service endpoints resolution.
- Frontend (vitest + RTL): reveal-on-click behavior, empty/error state rendering, search keyboard flow.
- Manual kind smoke: create a ConfigMap, Secret, Service+Ingress, PVC; verify masking/reveal, cross-links, search hit on each, and empty states in a fresh namespace.

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-7/<slug>`, open PR.
4. Print a concise summary: outcome + blockers only.
