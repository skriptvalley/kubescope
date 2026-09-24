# 0013. Session sign-in auth mode (`KUBESCOPE_AUTH_MODE=session`)

- **Status:** Accepted
- **Date:** 2026-09-24

## Context

[ADR-0005](0005-security-posture-read-only-and-secret-masking.md) shipped two auth modes: `none` and HTTP `basic`. Basic works, but it is a poor fit for a long-lived browser console:

- It uses the browser's native credential prompt.
- There is no sign-out: the browser caches credentials until it quits.
- Credentials travel on every request.
- Nothing expires.

The first shared deployment, in-cluster on projects.sujaykumar.dev ([ADR-0012](0012-sub-path-serving-behind-a-reverse-proxy.md)), wants a real sign-in page that reuses the operator's existing admin password. That instance runs **read-write with cluster-admin**. It was first gated by the proxy (Traefik ForwardAuth against another console's session), which made Kubescope depend on that console. The operator now wants Kubescope to carry its own gate and be developed against the live cluster.

FB-5 (OIDC, multi-user/hashed credentials) remains the richer auth story. This decision is the single-operator step before it.

## Decision

Add a third mode, **`session`**:

- **Credential.** The existing operator pair is reused, with no new env vars. `KUBESCOPE_AUTH_BASIC_PASSWORD` is required. `KUBESCOPE_AUTH_BASIC_USERNAME` is **optional**; when unset, sign-in is password-only, like an admin console. The `BASIC_` prefix is historical.
- **Endpoints.** `GET /api/v1/auth/session` returns `{mode, authenticated, usernameRequired}`. `POST` takes `{username?, password}` and sets the cookie. `DELETE` clears it. Outside session mode, GET reports `authenticated: true` and POST/DELETE return 404. The endpoints manage no cluster state, so they are exempt from the read-only guard.
- **Token.** Stateless `v1.<expiry>.<HMAC-SHA256>`. The HMAC key is **PBKDF2-SHA256(password, 600k iterations, salted with the username)**, derived once at startup, so the token survives restarts and redeploys. It expires server-side after **12 h**. A non-canonical expiry, or one further out than one TTL, is rejected. Changing the credential ends every session.
- **Cookie.** `kubescope_session`: HttpOnly, `SameSite=Lax`, `Path=<base path>/` (only Kubescope's own path gets it). `Secure` is set when the browser arrived over TLS, directly or via `X-Forwarded-Proto: https`.
- **Gate.** `/healthz`, the session endpoint and every non-`/api` path (the SPA shell and assets, which render the sign-in page) are public. Any other `/api` request without a valid cookie gets `401 {"code":"unauthenticated"}`. That covers SSE streams and the exec WebSocket, since cookies ride the handshake. Sign-in and sign-out are CSRF-protected by the existing cross-origin guard.
- **SPA.** `AuthGate` wraps the app. In session mode a signed-out visitor sees only the Dusk sign-in page; no layout renders and no cluster calls are made. Any `401 unauthenticated` answer anywhere, from an expired session or a rotated password, flips the cached session state via the query client's global error handler, and the gate returns to sign-in. Such answers are never retried. Sign-out drops every cached cluster response.

## Consequences

**Positive:**
- A proper sign-in and sign-out UX, with no browser credential prompt and a server-enforced expiry.
- No proxy dependency: one image, one env var switch, and the same credential model as `basic`.
- Sessions survive restarts, which matters for iterative deploys against a live cluster.

**Negative:**
- The key is derived from the password. A leaked cookie lets an attacker test password guesses offline at PBKDF2 cost, so a strong password matters. (A separate random signing secret would remove this, but costs a new env var. Deferred until multi-user or OIDC.)
- A stateless token cannot be revoked before expiry except by changing the credential. Sign-out clears the browser's copy, not a copied one.
- Single operator only; multi-user credentials remain FB-5.
- `BASIC_*` env names now also serve session mode.

## Alternatives considered

- **Keep proxy ForwardAuth only.** Rejected for this deployment: it couples Kubescope to another app's session and gives users of the plain image nothing.
- **Improve `basic` with a login page** (fetch-based Basic). Rejected: browsers still cache the Authorization header and there is still no sign-out. Changing basic's behaviour would also break scripted clients that rely on it.
- **Random in-memory signing key.** Rejected: every restart would sign everyone out, which is painful when deploying often.
- **OIDC now.** Rejected as out of scope. It needs an IdP; FB-5 tracks it.
