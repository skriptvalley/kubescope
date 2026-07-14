# 0005. Security posture: read-only mode and secret masking

- **Status:** Accepted
- **Date:** 2026-07-14

## Context

Kubescope can mutate and delete any resource, exec into pods, and read Secrets — with the full power of whatever credentials the kubeconfig carries. Exposed on a network without controls, it is a cluster-admin backdoor. v1 has no auth until Sprint 8, so the default posture must be safe-by-default.

## Decision

| Control | Behavior |
|---|---|
| Default bind | Bare binary binds `127.0.0.1:8080` (`KUBESCOPE_LISTEN_ADDR`) — localhost only |
| Docker image bind | Image sets `0.0.0.0:8080` via env. **Nuance:** inside a container, `127.0.0.1` would be unreachable from the host; the container boundary plus the explicitly published port (`-p 8080:8080`) *is* the isolation, so binding all interfaces inside the container is equivalent to the bare binary's localhost default |
| Read-only mode | `KUBESCOPE_READ_ONLY=true` rejects **all** mutating operations, enforced **server-side** (HTTP middleware in `internal/server`), with UI state reflecting it. UI-only gating is never sufficient |
| Secret values in logs | Never logged — no Secret `data`, tokens, or kubeconfig credentials in any log line, at any level |
| Secret values in UI | Masked by default; per-key **reveal-on-click** (Sprint 7). Raw-YAML views of Secrets mask `data` values too |
| Auth | `KUBESCOPE_AUTH_MODE`: `none` (default) \| `basic` \| `oidc` — basic/OIDC land in Sprint 8 |

**Warning (must appear in README and docs):** never expose Kubescope publicly without auth (`KUBESCOPE_AUTH_MODE`) **and** network controls (firewall, VPN, or reverse proxy). Until Sprint 8, that means: do not expose it beyond localhost/trusted networks at all.

## Consequences

**Positive:**
- Safe defaults: an unconfigured run is reachable only from the operator's own machine.
- Server-side read-only enforcement is bypass-proof regardless of UI or API-client behavior.
- Masking-by-default prevents shoulder-surfing and accidental screen-share leaks of Secret data.

**Negative:**
- Users who want LAN access must consciously override the bind address (bare binary) — friction by design.
- Read-only middleware must classify every route as mutating/non-mutating; misclassification is a security bug — covered by tests (Sprint 5, Story 5.4).
- No real authn/authz until Sprint 8; the interim story is network isolation only.

## Alternatives considered

- **Auth from day one** — rejected. Delays every sprint's demo loop for a tool that is localhost-only by default; scheduled deliberately for Sprint 8 hardening.
- **Read-only as the default** — rejected. Mutations are a core v1 use case; instead, mutations get guardrails (typed confirmation dialogs, Sprint 5) and an opt-in hard-off switch.
- **No reveal-on-click (never show Secret values)** — rejected. Reading a Secret is a legitimate, common debugging need; masking with explicit reveal balances utility and exposure.
