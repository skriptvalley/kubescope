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
- Squash-merge into `main`; keep the squash commit subject Conventional.
- Review feedback that outlives the PR goes into [../STATUS.md](../STATUS.md) under "Feedback / Review Tasks".

## Versioning & tags

- Semver tags: `v0.<sprint-milestone>.<patch>`. Pre-1.0 while v1 is in progress.
- First tagged release at Sprint 8 (`v0.1.0`); tag releases thereafter.

## CI (stub — implemented in Sprint 8, Story 8.3)

| Trigger | Pipeline |
|---|---|
| PR to `main` | lint + test + build |
| Tag `v*` | multi-arch image build + publish (`ghcr.io/skriptvalley/kubescope`) |
