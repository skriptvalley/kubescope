# Session Prompts

One Claude Code session per prompt. Each prompt is self-contained: a fresh session can execute it without re-reading the whole PRD.

Prompts are organised by **product line** (`v1/`, `v2/`), and within each by **`sprints/`** (the planned build-out) and **`feedback/`** (post-release features and review-driven mini-sprints).

```
prompts/
├── _template.md          # sprint-prompt skeleton — copy to start a new one
├── v1/                   # Sprints 0–8 + feedback through v1.0.0 GA (2026-07-21)
│   ├── sprints/          # sprint-0.md … sprint-8.md (canonical plan: docs/BUILD-PLAN.md)
│   └── feedback/         # FB-driven mini-sprints (FB-6, FB-8/FB-9, FB-11)
└── v2/                   # post-1.0 features
    ├── sprints/          # sprint-1 (FB-13 port-forward) · sprint-2 (FB-14 graph)
    └── feedback/         # FB-12 (e2e harness — the enabler)
```

## Which folder does a new prompt go in?

- **A planned sprint** in the canonical build-out → `v<line>/sprints/sprint-<N>.md`.
- **A post-release feature or a review/feedback item** (the pattern every post-GA feature has used — FB-6, FB-8, FB-11) → `v<line>/feedback/feedback-<slug>.md`.
- v2 work is tracked as FB items in [../STATUS.md](../STATUS.md): the two feature sprints under [v2/sprints/](v2/sprints/) (FB-13, FB-14) and the e2e enabler under [v2/feedback/](v2/feedback/) (FB-12).

## Workflow

1. **Start a fresh session and point it at the prompt** (`prompts/v<line>/…/<file>.md`). No carried-over context.
2. The session reads `STATUS.md` first (current state + pending feedback), then the prompt's context recap (architecture + ADRs).
3. It implements **only that prompt's stories**. No pulling work forward.
4. At the end it updates `STATUS.md` (last work + type, next expected, checkboxes) and opens a PR per [../docs/GIT-BRANCHING.md](../docs/GIT-BRANCHING.md).

## Review feedback loop

- PR-review comments and post-merge findings go into `STATUS.md` under **Feedback / Review Tasks** — one checkbox each, with source and priority.
- Feedback is picked up in a **dedicated feedback session** (point a fresh session at the FB item in `STATUS.md` + its `feedback/` prompt) or at the **start of the next sprint session**, before any new stories.

## v1 files

| File | Purpose |
|---|---|
| [v1/sprints/sprint-0.md](v1/sprints/sprint-0.md) … [sprint-8.md](v1/sprints/sprint-8.md) | One self-contained prompt per sprint; canonical plan in [../docs/BUILD-PLAN.md](../docs/BUILD-PLAN.md) |
| [v1/feedback/feedback-cluster-connectivity-and-onboarding.md](v1/feedback/feedback-cluster-connectivity-and-onboarding.md) | FB-6 (+ FB-1, FB-7 Story E): no-dead-end onboarding, failure taxonomy, runtime kubeconfig, live cluster loss/return |
| [v1/feedback/feedback-kubeconfig-registry-and-onboarding-polish.md](v1/feedback/feedback-kubeconfig-registry-and-onboarding-polish.md) | FB-8/FB-9: kubeconfig source registry (multi-file/dir) + onboarding polish (ADR-0008) |
| [v1/feedback/feedback-dusk-ui-redesign.md](v1/feedback/feedback-dusk-ui-redesign.md) | FB-11: Dusk design-system redesign (ADR-0009) |

## v2 files

| File | Purpose |
|---|---|
| [v2/sprints/sprint-1.md](v2/sprints/sprint-1.md) | FB-13: service-level port-forward — per-connection LB across a Service's endpoint pods |
| [v2/sprints/sprint-2.md](v2/sprints/sprint-2.md) | FB-14: resource relationship graph — `internal/graph` DTO + Cytoscape.js/fcose view (ns-scoped, focus + depth) |
| [v2/feedback/feedback-e2e-test-harness.md](v2/feedback/feedback-e2e-test-harness.md) | FB-12: e2e test harness — kind-first seed + docker-compose, opt-in EKS/Terraform profile |

Session rules (one sprint per session, ADR discipline, Definition of Done, STATUS.md updates) live in [../CLAUDE.md](../CLAUDE.md).
