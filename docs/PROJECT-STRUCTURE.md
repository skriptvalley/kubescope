# Kubescope — Project Structure

Final intended layout. The scaffold exists **now** with `.gitkeep` placeholders; directories fill in as sprints land (see [BUILD-PLAN.md](BUILD-PLAN.md)).

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
├── prompts/                # session prompts — v1|v2 × sprints/feedback (see prompts/README.md)
├── .claude/skills/         # project skills
├── Makefile · CLAUDE.md · AGENTS.md · STATUS.md · README.md
```

## Rationale & first population

| Path | Why it exists / why split this way | First populated |
|---|---|---|
| `cmd/kubescope/` | Standard Go layout: entrypoint + dependency wiring only, no logic. One binary → one `cmd` dir. | Sprint 0 (Story 0.1) |
| `internal/` | Compiler-enforced privacy: nothing outside the repo can import these packages. Split mirrors the [ARCHITECTURE.md](ARCHITECTURE.md) components 1:1, so each sprint touches a bounded package. | — |
| `internal/server/` | HTTP concerns isolated: chi routes, embedded-SPA fallback, middleware (read-only, auth). Handlers stay thin; domain logic lives elsewhere. | Sprint 0 (Stories 0.1, 0.3) |
| `internal/kube/` | Everything kubeconfig/context: parsing, per-context `rest.Config` + client caches, health. Sole owner of "which cluster am I talking to". | Sprint 0 (Story 0.4); fleshed out Sprint 1 |
| `internal/resources/` | Generic engine (discovery + dynamic client) and typed workload handlers together — both answer "get me resource data", one generically, one shaped for hot paths (ADR-0003). | Sprint 2 (generic); Sprint 3 (typed) |
| `internal/stream/` | Long-lived connections (SSE fan-out, WebSocket⇆SPDY exec) need lifecycle/backpressure handling distinct from request/response code (ADR-0006). | Sprint 4 (SSE watch/logs); Sprint 6 (exec/port-forward) |
| `internal/config/` | Single place that reads and validates `KUBESCOPE_*` env; the rest of the code takes a typed config struct. | Sprint 0 (Story 0.1) |
| `web/` | Separate because it has its own toolchain (Node 20+, Vite) and dependency tree. Built output is embedded into the Go binary at build time via `embed.FS` (ADR-0002) — never committed. | Sprint 0 (Story 0.2) |
| `build/` | Image definition: multi-stage, multi-arch Dockerfile + compose sample. Kept out of root to separate "how it's built" from "what it is". | Sprint 0 (Story 0.5) |
| `deploy/` | Run-time samples: run scripts, kind config for local smoke tests. Distinct from `build/`: consuming the image vs producing it. | Sprint 0 (Story 0.5) |
| `docs/` (+ `docs/adr/`) | PRD, architecture, build plan, git model; ADRs 0001–0006 record locked decisions. | Session 0 (now) |
| `prompts/` | Self-contained session prompts, organised `v<line>/{sprints,feedback}/` (see [../prompts/README.md](../prompts/README.md)). | Session 0 (now) |
| `.claude/skills/` | Repeated motions as skills: add-resource-view, write-adr, update-status, deploy-check. | Session 0 (now) |
| Root files | `Makefile` (dev commands), `CLAUDE.md` (agent rules), `AGENTS.md`, `STATUS.md` (session ledger), `README.md`. | Session 0; Makefile targets go real in Sprint 0 (Story 0.5) |

## Single module

One Go module at the root: `github.com/skriptvalley/kubescope`. One binary, one container, one release artifact — a multi-module workspace would add versioning and replace-directive overhead with zero payoff at this size. The frontend is not a Go package; it joins the artifact only through `embed.FS` at build time.
