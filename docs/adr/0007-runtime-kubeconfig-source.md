# 0007. Runtime kubeconfig source: path-only, opt-in

- **Status:** Superseded by [0008](0008-kubeconfig-source-registry.md)
- **Date:** 2026-07-17

## Context

First-run and broken-cluster states (FB-6) need a way to point Kubescope at a kubeconfig **without restarting the container**. Today the path is fixed at `kube.NewManager` from the env resolution in [0004](0004-cluster-auth-and-kubeconfig-in-docker.md). Letting an HTTP client change which credentials the server loads is a powerful capability that must not weaken the [0005](0005-security-posture-read-only-and-secret-masking.md) posture.

## Decision

`kube.Manager` gains a thread-safe, runtime-settable kubeconfig **path**, exposed as `PUT /api/v1/kubeconfig` with body `{"path": "/abs/path"}`.

- **Path-only in v1** — no pasted or uploaded kubeconfig content. Content held in memory would be a second, restart-lost credential source; upload paths risk writing secrets to disk or logs. A path keeps credentials in files the operator already controls. In Docker only a mounted path works — the UI surfaces that.
- **Opt-in:** the endpoint is enabled only when `KUBESCOPE_ALLOW_KUBECONFIG_SET=true` (new env var, default `false`; recorded here and in the `CLAUDE.md` canonical set). Disabled → `403 kubeconfig_set_disabled` with guidance.
- **Gating:** registered inside the read-only-guarded route group, so `KUBESCOPE_READ_ONLY=true` rejects it server-side; when `KUBESCOPE_AUTH_MODE` is set, auth applies as to every route.
- **Validate before swap:** the candidate path must be absolute, readable, parseable, and define ≥ 1 context — checked before the source is swapped; any failure returns a classified error and leaves the previous source intact. On success the per-context client cache and the in-memory active-context override are reset, and setup state / `/api/v1/contexts` reflect the new file immediately.
- **In-memory only:** a restart reverts to the env-resolved path — deliberate; a container restart returns to its declared configuration.
- **Secrets:** kubeconfig contents are never persisted elsewhere, never logged, and never echoed in error messages (paths and parse positions only). The mounted default remains read-only.

## Consequences

**Positive:**
- First-run onboarding can be fixed from the UI — no `docker rm` + remount loop.
- Validate-before-swap means a working source is never lost to a typo.
- Default-off keeps the pre-FB-6 attack surface unchanged for existing deployments.

**Negative:**
- A new env var grows the canonical set (accepted; this ADR records it).
- Path-only means Docker users must still mount the file before pointing at it.
- No path allowlist in v1 — any process-readable path can be named. Accepted because the endpoint is default-off, auth-gatable, and off under read-only; revisit an allowlist if multi-user deployments appear.

## Alternatives considered

- **Accept pasted/uploaded kubeconfig content (memory-only)** — rejected for v1: enlarges the secret-handling surface (request bodies, browser history, memory dumps), confusing restart semantics, and contradicts the single-source model of 0004.
- **Always-on endpoint (no flag)** — rejected: "point the server at arbitrary credentials" must be an explicit operator choice (0005 opt-in guardrail).
- **Path allowlist env var** — rejected for v1: a second knob for one feature; default-off plus auth covers the risk at this scale.
