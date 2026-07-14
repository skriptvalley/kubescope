# CLAUDE.md — Kubescope agent instructions

## Project

Kubescope: a web-based Kubernetes dashboard shipped as a single Docker container — mount your kubeconfig, switch contexts, browse and operate on every resource type (incl. CRDs).

Key docs: [docs/PRD.md](docs/PRD.md) · [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) · [docs/BUILD-PLAN.md](docs/BUILD-PLAN.md) · [docs/adr/](docs/adr/) · [STATUS.md](STATUS.md)

## Golden rules for every session

1. Read `STATUS.md` + the sprint's prompt in `prompts/` + any ADRs it references BEFORE starting.
2. One sprint per session. Do not pull work forward from later sprints.
3. Do not add dependencies, change architecture, or alter locked decisions without writing/updating an ADR and noting it in `STATUS.md`.
4. Conventional Commits; one logical change per commit; tests required for new logic (Definition of Done below).
5. Update `STATUS.md` at the end of every session (last work + type, next expected, task checkboxes). Non-negotiable.

## Tech stack + versions

| Layer | Choices |
|---|---|
| Backend | Go 1.23+, chi v5, client-go (discovery API + dynamic client for generic access; typed for hot paths), slog, coder/websocket (exec), SSE (watch/logs) |
| Frontend | React 18, TypeScript 5, Vite 5, TailwindCSS + shadcn/ui, TanStack Query v5, TanStack Table v8, react-router, CodeMirror 6 (YAML, Sprint 5), xterm.js 5 (terminal, Sprint 6) |
| Packaging | Single Go binary embedding built FE via `embed.FS`; multi-stage Dockerfile; multi-arch amd64 + arm64 |
| Testing | Go: table-driven + testify, controller-runtime envtest; FE: vitest + React Testing Library, Playwright later; kind for integration/smoke |

Go module: `github.com/skriptvalley/kubescope`. Image: `ghcr.io/skriptvalley/kubescope:latest`. Env vars are prefixed `KUBESCOPE_` — canonical set only: `KUBESCOPE_LISTEN_ADDR`, `KUBESCOPE_PORT`, `KUBESCOPE_KUBECONFIG`, `KUBESCOPE_READ_ONLY`, `KUBESCOPE_AUTH_MODE`. Do not invent new env vars.

## Repo map

```
kubescope/
├── cmd/kubescope/          # main.go — entrypoint, wiring (Sprint 0)
├── internal/
│   ├── server/             # http router, SPA serving, middleware
│   ├── kube/               # kubeconfig/context mgr, rest.Config, client caches
│   ├── resources/          # generic (discovery+dynamic) + typed workload handlers
│   ├── stream/             # SSE (watch/logs) + websocket (exec)
│   └── config/             # env config loading/validation
├── web/                    # React + Vite + TS app
│   └── src/
├── build/                  # Dockerfile, docker-compose sample
├── deploy/                 # sample run scripts, kind config
├── docs/                   # PRD, architecture, plans; docs/adr/ for ADRs
├── prompts/                # per-sprint session prompts
├── .claude/skills/         # project skills
├── Makefile · CLAUDE.md · AGENTS.md · STATUS.md · README.md
```

## Dev commands

Targets are stubs until Sprint 0 Story 0.5 makes them real.

| Target | When to use |
|---|---|
| `make dev` | Day-to-day development: run Go backend + Vite dev server together |
| `make fe-dev` | Frontend-only iteration (Vite dev server against a running backend) |
| `make build` | Produce the single `kubescope` binary (embeds built FE) |
| `make fe-build` | Build the production FE bundle into the embed path |
| `make test` | Go unit tests + envtest; run before every commit |
| `make fe-test` | vitest + React Testing Library suite |
| `make lint` | Go + TS linters; must be clean before PR |
| `make docker-build` | Build the multi-arch container image |
| `make docker-run` | Run the image locally with `~/.kube/config` mounted read-only |
| `make kind-up` | Create the local kind cluster (config in `deploy/`) for smoke tests |
| `make kind-down` | Delete the kind cluster |
| `make smoke` | Scripted smoke test of the container image against kind |

## Coding conventions

**Go**
- Package layout as in Repo map; all non-exported code under `internal/`.
- Wrap errors with context: `fmt.Errorf("listing %s: %w", gvr, err)`; never discard errors.
- Propagate `context.Context` through every request path and client call; honor cancellation.
- Structured logging via `slog` only; no `fmt.Println`, no log lines containing secret values.
- No global mutable state; inject dependencies through constructors.

**TypeScript**
- Functional components only; hooks over classes.
- All server state through TanStack Query — no `fetch` inside components.
- Single typed API client module; components consume typed hooks.
- Keep the frontend thin: aggregation/transformation happens in the Go backend.

## Definition of Done

- [ ] Compiles/builds (`make build`, `make fe-build`).
- [ ] Unit tests for new logic pass (`make test`, `make fe-test`).
- [ ] Lint clean (`make lint`).
- [ ] Manual smoke against kind for cluster-touching features.
- [ ] Docs updated if behavior/API changed; `STATUS.md` updated.
- [ ] PR opened.

## Git & branching

Full model: [docs/GIT-BRANCHING.md](docs/GIT-BRANCHING.md). Summary:
- Trunk-based; `main` protected and always releasable; short-lived branches `sprint-<N>/<story-slug>` (feedback: `fix/<slug>`).
- Conventional Commits: `feat|fix|refactor|test|docs|chore|build|ci(scope): subject`; one logical change per commit.
- One PR per story (or per sprint if solo and small); squash-merge; semver tags `v0.<sprint-milestone>.<patch>` from Sprint 8.
- PRs merge in the same session: agent review + green gates (`make test`/`lint`/`fe-test`; CI once Sprint 8 lands) → squash-merge → delete branch → sync local `main`. Sessions end clean on up-to-date `main`; a blocked merge is logged in `STATUS.md`.

## Security guardrails

- `KUBESCOPE_READ_ONLY=true` must reject ALL mutating operations server-side (middleware), not just hide UI.
- Never log secret values — not in slog, not in errors, not in debug output.
- Mask Secret data in the UI by default; reveal-on-click only.
- Default bind is `127.0.0.1:8080`; never change the default to a public bind. Docs must warn against exposing without auth.

## Session workflow

- **Start:** read `STATUS.md`, the sprint prompt in `prompts/`, and referenced ADRs.
- **During:** implement only that sprint's stories; write tests alongside logic.
- **End:** run `make test` + `make lint`, update `STATUS.md`, commit (Conventional Commits), open PR, agent-review the diff and fix findings, squash-merge once gates are green, sync local `main` (delete branch), print concise summary. End state: clean working tree on up-to-date `main`.

## Output style for Claude Code sessions

Ultra-concise: outcome + blockers only. No file listings, no step recaps, no "here's what I did" summaries unless explicitly asked. Default currency in any docs/examples is INR (₹).
