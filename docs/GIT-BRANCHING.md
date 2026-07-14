# Kubescope — Git & Branching

## Model

- Trunk-based development with short-lived branches.
- `main` is protected and always releasable — no long-lived `develop`/release branches.
- Branch from `main`, merge back via squash PR, delete the branch.

## Branch naming

| Purpose | Pattern | Examples |
|---|---|---|
| Sprint story | `sprint-<N>/<story-slug>` | `sprint-2/generic-resource-list`, `sprint-4/pod-log-streaming`, `sprint-0/walking-skeleton` |
| Feedback / review fix | `fix/<slug>` | `fix/context-switch-cache`, `fix/secret-masking-reveal` |

## Commit convention

Conventional Commits — `feat|fix|refactor|test|docs|chore|build|ci(scope): subject`. One logical change per commit.

```text
feat(resources): list any GVK via discovery + dynamic client
fix(kube): invalidate client cache on context switch
build(docker): multi-stage multi-arch image (amd64 + arm64)
docs(adr): accept 0006 — SSE for watch/logs, WebSocket for exec
```

## PR + review flow

- One PR per story; one PR per sprint is acceptable when working solo and the sprint is small.
- Self-review checklist = Definition of Done (see [../CLAUDE.md](../CLAUDE.md) and [CONTRIBUTING.md](CONTRIBUTING.md)).
- Every PR gets an **agent code review** of its diff before merge; real findings are fixed on the branch pre-merge, or logged as `FB-N` in [../STATUS.md](../STATUS.md) if they can wait.
- **Merge gate:** `make test`, `make lint`, `make fe-test` green. Until CI lands (Sprint 8, Story 8.3) these run locally and are the gate; after that, green CI on the PR is required.
- Squash-merge into `main`; keep the squash commit subject Conventional. Delete the branch on merge.
- Review feedback that outlives the PR goes into [../STATUS.md](../STATUS.md) under "Feedback / Review Tasks".

## Session-end state (non-negotiable)

A working session does not end with an open PR. The closing sequence is:

1. Gates green (test/lint/fe-test; CI once it exists).
2. Agent review done, findings resolved or logged.
3. Squash-merge the PR (Conventional subject), delete the remote branch.
4. Sync local: `git checkout main && git pull --prune`; delete the local branch.
5. End state: local `main` == `origin/main`, working tree clean — the next session starts from `main` with no leftovers.

If something genuinely blocks the merge (broken gate, unresolved review finding), leave the PR open, log the blocker in `STATUS.md` as the top item under "Feedback / Review Tasks", and say so in the session summary.

## Versioning & tags

- Semver tags: `v0.<sprint-milestone>.<patch>`. Pre-1.0 while v1 is in progress.
- First tagged release at Sprint 8 (`v0.1.0`); tag releases thereafter.

## CI (stub — implemented in Sprint 8, Story 8.3)

| Trigger | Pipeline |
|---|---|
| PR to `main` | lint + test + build |
| Tag `v*` | multi-arch image build + publish (`ghcr.io/skriptvalley/kubescope`) |
