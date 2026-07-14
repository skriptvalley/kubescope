# Contributing

## Setup

```sh
git clone https://github.com/skriptvalley/kubescope
cd kubescope
```

Prereqs: Go 1.23+, Node 20+, Docker, [kind](https://kind.sigs.k8s.io/) for smoke tests. Make targets are stubs until Sprint 0 (Story 0.5).

## Run

| Command | Use |
|---|---|
| `make dev` | Backend + Vite dev server together |
| `make fe-dev` | Frontend-only iteration against a running backend |

## Test

| Command | Covers |
|---|---|
| `make test` | Go unit tests + envtest (controller-runtime fake apiserver — first run downloads test binaries) |
| `make fe-test` | vitest + React Testing Library |
| `make lint` | Go + TS linters |

Cluster-touching features additionally need a manual smoke against a local kind cluster: `make kind-up`, exercise the feature, `make kind-down`.

## Branches & commits

Full model in [GIT-BRANCHING.md](GIT-BRANCHING.md). In short: trunk-based off protected `main`; branches `sprint-<N>/<story-slug>` (feedback: `fix/<slug>`); Conventional Commits (`feat|fix|refactor|test|docs|chore|build|ci(scope): subject`), one logical change per commit; one PR per story, squash-merged.

## Definition of Done

Mirrors [CLAUDE.md](../CLAUDE.md):

- [ ] Compiles/builds (`make build`, `make fe-build`).
- [ ] Unit tests for new logic pass (`make test`, `make fe-test`).
- [ ] Lint clean (`make lint`).
- [ ] Manual smoke against kind for cluster-touching features.
- [ ] Docs updated if behavior/API changed; [STATUS.md](../STATUS.md) updated.
- [ ] PR opened.
