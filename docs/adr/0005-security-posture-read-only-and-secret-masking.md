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
| Auth | `KUBESCOPE_AUTH_MODE`: `none` (default) \| `basic` \| `oidc`. `none` and `basic` ship in v1 (Sprint 8); `oidc` is reserved and **fails fast at startup** (not implemented) |
| Host allowlist | A Host-header allowlist middleware sits ahead of every route (DNS-rebinding defense, FB-3). See the Sprint 8 addendum |

**Warning (must appear in README and docs):** never expose Kubescope publicly without auth (`KUBESCOPE_AUTH_MODE=basic`) **and** network controls (firewall, VPN, or reverse proxy). The safest posture remains localhost/trusted networks only.

## Sprint 8 addendum — auth implementation and DNS-rebinding defense

### Basic-auth credential source (new config)

`KUBESCOPE_AUTH_MODE=basic` gates every route except `/healthz` with HTTP Basic auth. The credential source is a single operator username/password supplied via two new environment variables:

| Env var | Meaning |
|---|---|
| `KUBESCOPE_AUTH_BASIC_USERNAME` | Basic-auth username; required when mode is `basic` |
| `KUBESCOPE_AUTH_BASIC_PASSWORD` | Basic-auth password; required when mode is `basic` |

Rationale and rules:
- **Single operator/password** matches the v1 shape — one person running a localhost dashboard — and needs no user store or new dependency. Multi-user credentials, htpasswd files, and bcrypt hashing are deferred to v2 (revisit with OIDC).
- **Fail-fast validation:** `basic` with either variable missing/empty is a startup error, never a silent fallback to open. `oidc` is a startup error (not implemented).
- **Never logged:** credentials never appear in any log line. A failed attempt logs only path, remote address, and whether credentials were presented — never the submitted or configured values.
- **Constant-time comparison:** both username and password are compared with `subtle.ConstantTimeCompare` over SHA-256 digests, so neither value nor its length leaks via timing.
- **Plaintext-in-environment caveat:** the password lives in the process environment, so treat that environment as sensitive (documented in the README). This is acceptable for a self-hosted single-operator tool; hashed/file-based credentials are a v2 item.
- **Enforcement is server-side middleware** ahead of the whole route tree — the SPA is gated too (the browser's native Basic challenge drives the login), and a direct API call is rejected identically.

These additions supersede the "canonical env vars — do not invent new ones" note in `CLAUDE.md` for the auth credential source specifically; `CLAUDE.md`'s canonical list is updated to include them.

### Host-header allowlist (FB-3)

A localhost-bound, writable Kubescope is reachable by a **DNS-rebinding** page: a site the victim's browser loads at `attacker.example` whose DNS is flipped to `127.0.0.1` can drive exec/mutations, and a WebSocket-Origin check alone cannot stop it (the rebinding request looks same-origin to the socket). Mitigation: a Host-allowlist middleware ahead of every route.

- **Allowlist** = loopback names (`localhost`, `127.0.0.1`, `::1`) plus the concrete configured bind host, matched with or without a port. `/healthz` is exempt so probes are never rejected on Host grounds.
- **Wildcard binds** (`0.0.0.0` / `::` — the Docker image default, a deliberate "expose me" choice) have no enumerable hostname, so the guard is a pass-through in that mode; the operative protection there is `basic` auth + network controls. An empty request Host is allowed (non-browser clients, which rebinding cannot exploit, and which the loopback bind already fences).
- **Reverse-proxy caveat.** A proxy fronting the *loopback binary* connects to `127.0.0.1` but by default forwards the client's public `Host`, which the allowlist rejects. Operators must either set the proxy to send `Host: localhost` (e.g. nginx `proxy_set_header Host localhost;`) or bind Kubescope to a concrete address the proxy targets. Documented in the README rather than solved with a new allowed-hosts config var, keeping the canonical env set small; a configurable allowlist is a candidate if proxy deployments become common.

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
