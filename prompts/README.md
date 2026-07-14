# Sprint Prompts

One Claude Code session per sprint. Each `sprint-N.md` is a self-contained prompt: a fresh session can execute it without re-reading the whole PRD.

## Workflow

1. **Start a new session and paste/point it at `prompts/sprint-N.md`.** Always a fresh session — no carried-over context.
2. The session reads `STATUS.md` first (current state + pending feedback tasks), then the prompt's context recap (architecture section + ADRs).
3. The session implements **only that sprint's stories**. No pulling work forward from later sprints.
4. At the end of the session it updates `STATUS.md` (last work + type, next expected, checkboxes) and opens a PR per [docs/GIT-BRANCHING.md](../docs/GIT-BRANCHING.md).

## Review feedback loop

- PR review comments and post-merge findings go into `STATUS.md` under **Feedback / Review Tasks** — one checkbox each, with source and priority.
- Feedback is picked up either in a **dedicated feedback session** (point a fresh session at the feedback list in `STATUS.md`) or at the **start of the next sprint session**, before any new stories.

## Files

| File | Purpose |
|---|---|
| [_template.md](_template.md) | Skeleton every sprint prompt follows — copy it to create a new one |
| `sprint-0.md` … `sprint-8.md` | One self-contained prompt per sprint; canonical plan in [docs/BUILD-PLAN.md](../docs/BUILD-PLAN.md) |

Session rules (one sprint per session, ADR discipline, Definition of Done, STATUS.md updates) live in [CLAUDE.md](../CLAUDE.md).
