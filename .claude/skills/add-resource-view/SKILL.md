---
name: add-resource-view
description: Step-by-step to add end-to-end support for a new Kubernetes resource type — backend handler, API contract, frontend list/detail/YAML, nav, tests.
---

# Add a resource view

The repeated motion of this project. Follow the steps in order; all paths are from the repo layout in `docs/PROJECT-STRUCTURE.md`.

## 1. Decide: generic vs typed

| Path | When | Backend cost |
|---|---|---|
| **Generic (default)** — discovery + dynamic client | Any GVK, incl. CRDs; list/detail/YAML needs no computed fields | Zero code beyond a list-column config entry |
| **Typed handler** — client-go typed client | Hot paths needing computed status (e.g. Pod restarts, Deployment rollout state) | New handler in `internal/resources` |

Boundary and rationale: `docs/adr/0003-generic-resource-access-via-discovery-and-dynamic-client.md`. When in doubt start generic; promote to typed only when the UI needs a column or summary the raw object does not carry.

## 2. Backend

- [ ] Route registration in `internal/server` — skip if the generic wildcard route already covers the GVK; typed handlers get an explicit route.
- [ ] Handler in `internal/resources` — typed path only. Generic path reuses the dynamic-client engine as-is.
- [ ] Server-side list-column config in `internal/resources`: column id, header, value extractor (JSONPath or Go func for typed), sort hint. The backend shapes rows so the frontend stays thin.

## 3. API contract

Match the existing generic-resource endpoints registered in `internal/server` — verify the actual prefix before adding:

| Endpoint | Returns |
|---|---|
| `GET .../resources/{group}/{version}/{resource}` (+ `?namespace=`) | `{ columns: [...], rows: [...] }` — rows pre-shaped by the column config |
| `GET .../resources/{group}/{version}/{resource}/{name}` | object + metadata; typed handlers add a computed summary block |
| `GET .../resources/{group}/{version}/{resource}/{name}/yaml` | raw manifest YAML |

Shape conventions: cluster-scoped resources take no `namespace` param; JSON fields camelCase; errors as `{ error, code }`; Secret values never appear in list rows or logs (ADR-0005 posture).

## 4. Frontend (`web/src`)

- [ ] TanStack Table (v8) column defs mirroring the server column config; cell formatting lives here, data shaping stays server-side.
- [ ] Detail view (summary tab) + raw YAML tab.
- [ ] All data via the typed API client wrapped in TanStack Query (v5) hooks. **Never fetch-in-component** — no raw `fetch` in components, no server state outside Query.
- [ ] Query keys include context + GVK + namespace so cache invalidation on context switch works.

## 5. Nav registration

- Discovery-driven GVKs appear in the sidebar automatically (nav is built from the discovery API, Sprint 2).
- For a curated entry (e.g. the workloads section), add a manual pin where the sidebar groups are defined in `web/src`.

## 6. Tests

| Layer | Requirement |
|---|---|
| Go | Table-driven tests (+ testify) for column extractors / typed summary logic |
| Go, API-touching | envtest (controller-runtime) against the fake apiserver for list/detail/yaml handlers |
| Frontend | vitest for column defs and formatting logic |
| Manual | kind smoke: list, detail, and YAML tab render for the new type |

## 7. Wrap up

- [ ] Update `STATUS.md` per `.claude/skills/update-status/SKILL.md`.

## Final checklist

- [ ] Generic vs typed decided (ADR-0003 boundary applied)
- [ ] Backend: route (if needed) + handler (if typed) + column config
- [ ] API: list/detail/yaml respond with the conventional shapes
- [ ] Frontend: table columns, detail view, YAML tab
- [ ] Data flows through typed API client + TanStack Query only
- [ ] Sidebar entry present (discovery or manual pin)
- [ ] Go table-driven + envtest + vitest all pass
- [ ] STATUS.md updated
