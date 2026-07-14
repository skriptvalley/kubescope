# 0003. Generic resource access via discovery and dynamic client

- **Status:** Accepted
- **Date:** 2026-07-14

## Context

v1 must let users browse **every** resource type in a cluster — core resources **and CRDs**, namespaced and cluster-scoped ([../PRD.md](../PRD.md)). CRDs are unknown at compile time, so a purely typed `client-go` approach cannot cover them. We need one engine that handles any GVK, without giving up rich views for common workloads.

## Decision

- **Discovery API** enumerates all API groups/versions/resources — including CRDs — at runtime; results are cached and refreshable (Sprint 2). The sidebar nav and generic routes are built from this.
- **Dynamic client** performs generic `get` / `list` / `watch` / `patch` / `delete` against any GVR, handling cluster- vs namespace-scoped resources uniformly. Objects flow as `unstructured.Unstructured`.
- **Typed client-go handlers only for hot paths**: workload summaries (Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs — Sprint 3), log streaming, and exec, where typed structs and subresource APIs earn their keep.
- **Informers** back watch/cache where useful — the watch→SSE bridge ([0006](0006-live-updates-sse-and-streaming-websocket.md)) and frequently-listed resources.
- Both paths live in `internal/resources/` ([../ARCHITECTURE.md](../ARCHITECTURE.md)).

## Consequences

**Positive:**
- Any resource type — including CRDs installed after Kubescope starts (post-refresh) — is browsable with zero per-type code.
- New resource "support" for hot paths is additive: the generic engine already covers reads; typed handlers only add richer views.
- Watch and mutation semantics are uniform across all types.

**Negative:**
- Unstructured objects mean map-navigation on the backend; per-type field access is untyped and needs care.
- List **column config must live server-side per resource** (which fields to surface, how to render them) — the thin frontend ([0001](0001-tech-stack-and-build-from-scratch.md)) renders columns the API describes, with a sane generic default (name, namespace, age).
- Discovery caching adds staleness/refresh logic to own.

## Alternatives considered

- **Typed clients only** — rejected. Cannot cover CRDs or any type unknown at compile time; contradicts the core "all resources" requirement.
- **Shelling out to `kubectl`** — rejected. Requires bundling the binary in the container, forks a process per request, parses text output, and loses watch semantics and structured errors. The dynamic client is the same capability as a library.
