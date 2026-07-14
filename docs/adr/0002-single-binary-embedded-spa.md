# 0002. Single binary with embedded SPA

- **Status:** Accepted
- **Date:** 2026-07-14

## Context

Kubescope's core promise is "pull one image, run one container" ([../PRD.md](../PRD.md)). The stack has two build outputs — a Go API server and a React SPA ([0001](0001-tech-stack-and-build-from-scratch.md)). Shipping them separately would mean two processes or two containers, CORS between origins, and a more complicated run story.

## Decision

- The Vite production build output is embedded into the Go binary via `embed.FS`.
- One `kubescope` binary serves both the API (under `/api/...`) and the SPA (static assets + SPA fallback to `index.html` for client-side routes).
- One process, one container. The Docker image ([../ARCHITECTURE.md](../ARCHITECTURE.md)) is a multi-stage build: Node builds the FE → Go compiles the binary embedding it → minimal runtime stage.

## Consequences

**Positive:**
- Single artifact to version, distribute, and run — `docker run` and it works.
- Same origin for SPA and API: no CORS, cookies/headers just work, relative API URLs.
- Trivially self-hostable; the bare binary alone is a complete deployment.

**Negative:**
- Multi-stage Docker build is **required**; local builds need both Go and Node toolchains.
- Any FE change requires a Go rebuild to re-embed assets (dev mode uses Vite dev server + API proxy to avoid this loop).
- No independent FE deploy or CDN hosting — FE and BE version together, always.

## Alternatives considered

- **Two containers (API + nginx serving the SPA)** — rejected. Doubles the run/compose story, adds CORS and inter-container networking, breaks the one-command promise for zero benefit at this scale.
- **Separate static host (CDN/object storage) for the SPA** — rejected. Adds a deploy target and origin split for a self-hosted local tool; independent FE releases are a non-goal in v1.
