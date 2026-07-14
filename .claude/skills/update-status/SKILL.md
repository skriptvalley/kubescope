---
name: update-status
description: Exact rules for updating STATUS.md at the end of every session — fields, checkboxes, sprint status, feedback items.
---

# Update STATUS.md

Mandatory at the END of every session (golden rule in `CLAUDE.md`). Structure of the file is fixed — edit values, never sections.

## "Current state" fields

| Field | Rule | Example |
|---|---|---|
| Last updated | Today's date, `YYYY-MM-DD` | `2026-07-14` |
| Last work | What completed + type tag (below) | `Sprint 2 — Generic resource engine (read-only) [sprint]` |
| Summary | 1–2 lines, outcomes only | `Generic list/detail live incl. CRDs; nav from discovery.` |
| Next expected | Next sprint title verbatim, or the top feedback task if it blocks | `Sprint 3 — Workload deep views` / `FB-2 (hi), then Sprint 3` |
| ADRs touched this session | `none` or the numbers | `0007` |

## "Last work" type tag

- `[sprint]` — the session executed a sprint prompt from `prompts/`. Use the sprint title verbatim from the sprint board. Example: `Sprint 1 — Kubeconfig & context management + cluster overview [sprint]`.
- `[feedback]` — the session worked items from "Feedback / Review Tasks". Example: `FB-1, FB-3 — context-switch race + masked-secret regression [feedback]`.
- A partially finished sprint is still `[sprint]`; say what remains in Summary and leave the sprint `in-progress`.

## Sprint board

- Tick task checkboxes (`- [ ]` → `- [x]`) as tasks complete. Never reword, reorder, or remove them.
- Flip sprint status in the heading: `todo` → `in-progress` (first task ticked) → `done` (all tasks ticked and exit criteria met).
- Never delete history or completed items — only tick. Correct a wrong entry with a short parenthetical note, not deletion.

## Feedback / Review Tasks

- Format: `- [ ] FB-N: <description> (source: <sprint-N review | manual smoke | user>, priority: <hi|med|lo>)`.
- `N` is the next unused number; never reuse numbers, even after completion.
- Replace the `_None yet._` placeholder with the first real item; after that, append.
- Tick completed feedback items; do not delete them.

## Checklist

- [ ] Last updated refreshed
- [ ] Last work set with `[sprint]` or `[feedback]`
- [ ] Next expected set
- [ ] Task checkboxes ticked; sprint status flipped if warranted
- [ ] New feedback logged as `FB-N: ... (source, priority)`
- [ ] ADRs touched recorded
- [ ] Nothing deleted — history intact
