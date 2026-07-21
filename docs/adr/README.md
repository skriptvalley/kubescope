# Architecture Decision Records

Decisions that lock Kubescope's technical direction. Referenced from [../ARCHITECTURE.md](../ARCHITECTURE.md) and the sprint prompts. To author a new ADR, use the `write-adr` skill.

## Index

| # | Title | Status | Date |
|---|---|---|---|
| 0001 | [Tech stack and build from scratch](0001-tech-stack-and-build-from-scratch.md) | Accepted | 2026-07-14 |
| 0002 | [Single binary with embedded SPA](0002-single-binary-embedded-spa.md) | Accepted | 2026-07-14 |
| 0003 | [Generic resource access via discovery and dynamic client](0003-generic-resource-access-via-discovery-and-dynamic-client.md) | Accepted | 2026-07-14 |
| 0004 | [Cluster auth and kubeconfig in Docker](0004-cluster-auth-and-kubeconfig-in-docker.md) | Accepted | 2026-07-14 |
| 0005 | [Security posture: read-only mode and secret masking](0005-security-posture-read-only-and-secret-masking.md) | Accepted | 2026-07-14 |
| 0006 | [Live updates via SSE, streaming via WebSocket](0006-live-updates-sse-and-streaming-websocket.md) | Accepted | 2026-07-14 |
| 0007 | [Runtime kubeconfig source: path-only, opt-in](0007-runtime-kubeconfig-source.md) | Superseded by 0008 | 2026-07-17 |
| 0008 | [Kubeconfig source registry: files + directories, kubectl merge](0008-kubeconfig-source-registry.md) | Accepted | 2026-07-17 |
| 0009 | [Adopt the skriptvalley "Dusk" design system](0009-dusk-design-system.md) | Accepted | 2026-07-21 |
| 0010 | [EKS e2e via a host-minted static token-kubeconfig](0010-e2e-eks-static-token-kubeconfig.md) | Accepted | 2026-07-21 |

## Template (MADR-style)

```markdown
# <NNNN>. <Title>

- **Status:** Proposed | Accepted | Superseded by 000X
- **Date:** YYYY-MM-DD

## Context
<What forces this decision? Constraints, requirements, prior art.>

## Decision
<What we are doing, stated as fact.>

## Consequences
**Positive:**
- <benefit>

**Negative:**
- <cost / risk we accept>

## Alternatives considered
- **<Alternative>** — <why rejected>
```

## Lifecycle

New ADRs start as `Proposed`, become `Accepted` when the decision is adopted, and are never deleted.
If a later decision replaces one, the old ADR's status becomes `Superseded by 000X` and both link to each other.
