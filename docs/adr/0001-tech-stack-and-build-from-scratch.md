# 0001. Tech stack and build from scratch

- **Status:** Accepted
- **Date:** 2026-07-14

## Context

Kubescope is a web-based Kubernetes dashboard shipped as a single Docker container ([../PRD.md](../PRD.md)). The owner is a **backend engineer** whose strength is Go; frontend complexity is a velocity risk. We must choose: build from scratch or extend an existing dashboard, and pick a stack that keeps most of the complexity server-side.

## Decision

Build from scratch with:

| Layer | Choice |
|---|---|
| Backend language | Go 1.23+ |
| Kubernetes access | `client-go` (typed) + discovery API + dynamic client ([0003](0003-generic-resource-access-via-discovery-and-dynamic-client.md)) |
| HTTP routing | `net/http` + chi v5 |
| Logging | `slog` (structured) |
| Frontend | React 18 + TypeScript 5 + Vite 5 |
| Styling / components | TailwindCSS + shadcn/ui |
| Server state | TanStack Query v5 (polling, caching, invalidation) |
| Tables | TanStack Table v8 |
| Routing (FE) | react-router |
| Later sprints | CodeMirror 6 (YAML, Sprint 5), xterm.js 5 (terminal, Sprint 6) |

**Thin-frontend principle:** the Go backend does aggregation, transformation, and shaping; the UI renders what the API returns. Push complexity server-side so the React layer stays simple — this matches the owner's skills and keeps FE code shallow.

## Consequences

**Positive:**
- Full ownership of architecture, API contracts, and product direction; no upstream constraints.
- Go-heavy division of labor fits the owner; FE stays a thin render layer.
- Mature, well-documented libraries throughout; TanStack Query gives polling/caching before live updates land ([0006](0006-live-updates-sse-and-streaming-websocket.md)).

**Negative:**
- We rebuild table stakes (resource lists, YAML views) that existing dashboards already have.
- Two toolchains (Go + Node) in the build ([0002](0002-single-binary-embedded-spa.md)).
- React/TS is still a learning cost for a backend engineer, mitigated but not eliminated by the thin-frontend principle.

## Alternatives considered

- **Extend Headlamp via a plugin** — rejected. Plugin development is React/TS-heavy, fighting the owner's Go strength exactly where we want leverage. Headlamp desktop already covers local multi-cluster use, so extending it adds little ownership or learning value.
- **htmx + templ (Go-only frontend)** — rejected. Terminal/exec (xterm.js), streaming log views, and rich table interactions are much cleaner in React. **Noted as the fallback** if frontend velocity stalls: the thin-frontend API design keeps this door open.
