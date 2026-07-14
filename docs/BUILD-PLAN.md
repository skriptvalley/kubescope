# Kubescope — Build Plan

Scope and rationale: [PRD.md](PRD.md) · Components: [ARCHITECTURE.md](ARCHITECTURE.md) · Live progress: [../STATUS.md](../STATUS.md)

## Sprint plan (v1)

| Sprint | Goal | Key stories | Exit criteria |
|---|---|---|---|
| **0 — Walking skeleton & deployment spine** | Prove the whole model end-to-end | Go module & HTTP server skeleton, Frontend scaffold, Single binary (embedded FE + SPA fallback), Prove the model (kubeconfig → node list), Multi-arch Dockerfile + Makefile + kind config | `docker run` with mounted kubeconfig shows the cluster's node list in the browser |
| **1 — Kubeconfig & context management + cluster overview** | First-class multi-context support | Kubeconfig parsing & context enumeration API, Context switching + per-context client cache, Per-context connection/health status, Cluster overview page | Switch between ≥2 contexts and see per-cluster overview |
| **2 — Generic resource engine (read-only)** | Browse ANY resource type incl. CRDs | Discovery incl. CRDs, Dynamic client get/list + scope handling, Generic resource list UI, Generic resource detail view + raw YAML tab | Browse any resource type read-only, including a CRD |
| **3 — Workload deep views** | Rich, resource-appropriate views | Typed backend workload summaries, Pod detail, Controller views, Related events | Deep views for all listed workloads |
| **4 — Live updates + logs + events** | The dashboard feels live | Watch→SSE bridge, Live-updating lists/details in UI, Pod log streaming, Events feed | Lists auto-update on change; pod logs stream live |
| **5 — Mutations + guardrails** | Safe write operations | Edit YAML + apply, Scale/rollout-restart/delete, Node cordon/uncordon/drain, `KUBESCOPE_READ_ONLY` enforcement + Secret masking | Can mutate resources safely; read-only mode blocks all writes |
| **6 — Exec terminal + port-forward** | Operate inside pods from the browser | WebSocket exec bridge, xterm.js terminal UI, Port-forward | Working in-browser terminal + port-forward |
| **7 — Config/networking/RBAC/storage + polish** | Broad resource coverage + usable UX | ConfigMaps + Secrets, Services + Ingress views, RBAC, Storage, Global search + empty/error states + keyboard nav | Coverage + good empty/error states |
| **8 — Hardening & release** | Ship v0.1.0 | Optional auth, Security pass, CI + multi-arch image publish, v0.1.0 release | Tagged v0.1.0, published multi-arch image, complete docs |

Full story scope, acceptance criteria, and task checklists live in the per-sprint prompts (`prompts/sprint-0.md` … `sprint-8.md`).

## v2 backlog

- Resource graph (ownerReferences + selectors + config/secret/volume refs → interactive graph)
- Metrics via metrics-server (CPU/mem on pods/nodes)
- Side-by-side multi-cluster view
- Plugin/extension system

## Execution model

One sprint = one Claude Code session, driven by that sprint's prompt in `prompts/` (workflow in [../prompts/README.md](../prompts/README.md)).
Each session implements only its sprint's stories, updates [../STATUS.md](../STATUS.md), and opens a PR; review feedback lands in STATUS.md's "Feedback / Review Tasks" for a feedback session or the next sprint.
