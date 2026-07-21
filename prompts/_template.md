<!-- Sprint prompt template. To instantiate: copy to prompts/v<line>/sprints/sprint-<N>.md and replace every <angle-bracket> field.
Keep the headings and their order exactly as-is; scope stories strictly to the canonical breakdown in ../docs/BUILD-PLAN.md.
Each story gets 1-3 lines of scope + 3-6 acceptance criteria; keep the whole prompt self-contained and tight (~80-140 lines). -->

# Sprint <N> — <Title>

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching.
3. ADRs: <list the ADR numbers this sprint depends on>.
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
<one sentence>

## Stories
### Story <N.x> — <name>
<1–3 lines of scope>
**Acceptance criteria:**
- [ ] <criterion>

## Task checklist
- [ ] <task>

## Testing requirements
<what must be covered by unit tests / envtest / manual kind smoke>

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-<N>/<slug>`, open PR.
4. Print a concise summary: outcome + blockers only.
