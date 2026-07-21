# Sprint 0 — Walking skeleton & deployment spine

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching (this sprint spans every component boundary at skeleton depth).
3. ADRs: `docs/adr/0001-tech-stack-and-build-from-scratch.md` (stack), `docs/adr/0002-single-binary-embedded-spa.md` (embedded SPA, one container), `docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md` (mounted kubeconfig + local-cluster networking gotchas).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Prove the whole model end-to-end: `docker run` with a mounted kubeconfig shows the cluster's node list in the browser.

## Stories
### Story 0.1 — Go module & HTTP server skeleton
Bootstrap the Go module and a minimal HTTP server: `cmd/kubescope` entrypoint, chi router, slog, `/healthz`, env config in `internal/config`.
**Acceptance criteria:**
- [ ] `go mod init github.com/skriptvalley/kubescope` done (Go 1.23+); `go build ./...` succeeds.
- [ ] `GET /healthz` returns 200 with a small JSON body.
- [ ] `internal/config` loads `KUBESCOPE_LISTEN_ADDR` (default `127.0.0.1:8080`), `KUBESCOPE_PORT` (overrides the port part of `LISTEN_ADDR`), `KUBESCOPE_KUBECONFIG` (default `/kubeconfig` → `$KUBECONFIG` → `~/.kube/config`), with validation.
- [ ] slog structured logging: one startup line + chi request-logging middleware. No global mutable state.

### Story 0.2 — Frontend scaffold
Scaffold `web/` with Vite 5 + React 18 + TypeScript 5, TailwindCSS + shadcn/ui, TanStack Query v5, react-router.
**Acceptance criteria:**
- [ ] `web/` builds to `web/dist` via the FE build (Node 20+).
- [ ] Base app layout renders with Tailwind + shadcn/ui components.
- [ ] TanStack Query provider + react-router wired with an initial route.
- [ ] Typed API client module exists; components never call `fetch` directly.

### Story 0.3 — Single binary: embed built FE via `embed.FS`, SPA fallback serving
Embed `web/dist` into the Go binary and serve the SPA plus API from one process.
**Acceptance criteria:**
- [ ] `make build` produces one self-contained `kubescope` binary serving the SPA at `/`.
- [ ] Unknown non-API paths fall back to `index.html`; `/healthz` and `/api/*` are unaffected.
- [ ] FE assets are embedded at compile time — no runtime filesystem dependency.

### Story 0.4 — Prove the model: load mounted kubeconfig, list nodes via client-go, render node list in UI
Load the mounted kubeconfig, list nodes via client-go for the current context, render the list in the UI.
**Acceptance criteria:**
- [ ] `internal/kube` loads the kubeconfig from the configured path and builds a `rest.Config` for the current context (embedded certs/tokens work as-is — ADR-0004).
- [ ] `GET /api/v1/nodes` returns name, status, and Kubernetes version per node via client-go.
- [ ] UI node-list page renders the data through TanStack Query.
- [ ] Missing/unreadable kubeconfig returns a structured JSON error; the server stays up.

### Story 0.5 — Multi-stage multi-arch Dockerfile + real Makefile targets + kind config in deploy/
Containerize and wire the dev loop: Dockerfile in `build/`, real Make targets, kind config in `deploy/`.
**Acceptance criteria:**
- [ ] Multi-stage Dockerfile: node builds FE → go builds binary embedding FE → minimal runtime (distroless or alpine).
- [ ] Multi-arch (amd64 + arm64) build target via `docker buildx`; image tagged `ghcr.io/skriptvalley/kubescope:latest`.
- [ ] Image sets `KUBESCOPE_LISTEN_ADDR=0.0.0.0:8080` (container boundary is the isolation — ADR-0005 default stays localhost for the bare binary).
- [ ] Makefile stubs replaced with real targets: `dev`, `fe-dev`, `build`, `fe-build`, `test`, `fe-test`, `lint`, `docker-build`, `docker-run`, `kind-up`, `kind-down`, `smoke` — matching CLAUDE.md's Dev commands table.
- [ ] `deploy/` contains a kind cluster config + sample run script.

## Task checklist
- [ ] `go mod init github.com/skriptvalley/kubescope`; add chi v5, client-go, testify.
- [ ] Remove `.gitkeep` from every directory this sprint populates (`cmd/`, `internal/*`, `web/`, `build/`, `deploy/`).
- [ ] `internal/config`: env loading + validation with canonical defaults.
- [ ] `cmd/kubescope/main.go`: wire config → slog → chi router → `/healthz`.
- [ ] Scaffold `web/`: Vite + React 18 + TS + Tailwind + shadcn/ui + TanStack Query + react-router.
- [ ] Typed FE API client + node-list page.
- [ ] `internal/server`: `embed.FS` of `web/dist` + SPA fallback handler.
- [ ] `internal/kube`: kubeconfig load + `rest.Config` for the current context.
- [ ] `GET /api/v1/nodes` handler (typed clientset).
- [ ] `build/Dockerfile`: multi-stage, multi-arch, runtime env `KUBESCOPE_LISTEN_ADDR=0.0.0.0:8080`.
- [ ] Replace Makefile stubs with the real targets from Story 0.5.
- [ ] `deploy/`: kind config + sample run script.
- [ ] Manual smoke: kind cluster up, then `docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest` → node list at http://localhost:8080 (local clusters need `--network host` on Linux or a `host.docker.internal` rewrite on Mac/Win — ADR-0004).

## Testing requirements
| Layer | Must cover |
|---|---|
| Table-driven Go tests | `internal/config` defaults/overrides/precedence (`LISTEN_ADDR` vs `PORT`, kubeconfig fallback chain); SPA fallback routing (API vs asset vs unknown path) |
| envtest | `/api/v1/nodes` handler against a fake apiserver (API-touching) |
| vitest | Typed API client behavior; node-list page rendering states (loading/data/error) |
| Manual kind smoke | The exit criterion: `docker run` with mounted kubeconfig shows the node list in the browser end-to-end |

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-0/<story-slug>`, open PR.
4. Agent code review on the PR diff; fix real findings on the branch (or log them as FB-N).
5. When gates are green (`make test` + `make lint` + `make fe-test`; green CI once Sprint 8 lands): squash-merge with a Conventional subject, delete the branch, sync local `main` (`git checkout main && git pull --prune`).
6. Print a concise summary: outcome + blockers only. The session ends with the work merged and the repo clean on up-to-date `main`.
