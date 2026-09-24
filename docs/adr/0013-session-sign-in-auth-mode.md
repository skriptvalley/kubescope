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
- **Token.** Stateless `v1.<expiry>.<HMAC-SHA256>`. The HMAC key is HMAC(**`KUBESCOPE_AUTH_SESSION_KEY`**, PBKDF2-SHA256(password, 600k iterations, salted with the username)), derived once at startup, so the token survives restarts and redeploys.
  - The session key is an optional random secret. It is recommended whenever the password is shared or might be weak: without it, a leaked cookie lets an attacker test password guesses offline at PBKDF2 cost. It also binds tokens to one deployment.
  - The token expires server-side after **12 h**. A non-canonical expiry, or one further out than one TTL, is rejected. Changing the credential or the key ends every session.
- **Cookie.** `kubescope_session`: HttpOnly, **`SameSite=Strict`** (free: every authenticated request is a same-origin fetch from the public shell), `Path=<base path>/`. `Secure` is set when the browser arrived over TLS, directly or via `X-Forwarded-Proto: https`. Every cookie of that name is tried, so a stray one with a longer path can't shadow the real one.
- **Gate.** `/healthz`, the session endpoint and every non-`/api` path (the SPA shell and assets, which render the sign-in page) are public. Any other `/api` request without a valid cookie gets `401 {"code":"unauthenticated"}`. That covers SSE streams and the exec WebSocket, since cookies ride the handshake.
  - An admitted request's context ends at the token's expiry, so a stream or shell can't outlive its session.
  - Every `/api` response is `Cache-Control: no-store`.
  - A session mode without its state fails closed with a 503.
- **Guessing.** Failed sign-ins are capped **process-wide**: a burst of 10, then 10/min, with `429` + `Retry-After`. A per-client limit would trust spoofable forwarding headers. Successes don't spend the budget, and each failure also sleeps 400 ms. Sign-in and sign-out are CSRF-protected by the cross-origin guard.
- **Exec origins.** The `localhost:*` WebSocket origins (for the Vite dev proxy) are admitted only when Kubescope is bound to loopback. Cookies ignore ports, so on an exposed or port-forwarded instance another localhost page must not be able to open a shell.
- **SPA.** `AuthGate` wraps the app. In session mode a signed-out visitor sees only the Dusk sign-in page; no layout renders and no cluster calls are made.
  - **Expired sessions.** Any `401 unauthenticated` answer, from an expired session or a rotated password, flips the cached session state via the query client's global error handler, and the gate returns to sign-in. Such answers are never retried. Cached state wins over a failed background refetch.
  - **Sign-in** confirms that the cookie stuck before mounting the app, which prevents a silent loop.
  - **Sign-out** flips the gate only on success, then drops every cached query and mutation, including the sign-in mutation, which is kept only while its page is mounted.

## Consequences

**Positive:**
- A proper sign-in and sign-out UX, with no browser credential prompt and a server-enforced expiry.
- No proxy dependency: one image, one env var switch, and the same credential model as `basic`.
- Sessions survive restarts, which matters for iterative deploys against a live cluster.

**Negative:**
- Without `KUBESCOPE_AUTH_SESSION_KEY`, the key is derived from the password alone, and a leaked cookie allows offline guessing at PBKDF2 cost. The key is one optional env var; deployments sharing a password should set it.
- The process-wide limiter lets a determined guesser keep sign-in rate-limited, locking the operator out while an attack runs. That is a denial of service rather than a breach, and the right trade for a single-operator console.
- A stateless token cannot be revoked before expiry except by changing the credential. Sign-out clears the browser's copy, not a copied one.
- Single operator only; multi-user credentials remain FB-5.
- `BASIC_*` env names now also serve session mode.

## Alternatives considered

- **Keep proxy ForwardAuth only.** Rejected for this deployment: it couples Kubescope to another app's session and gives users of the plain image nothing.
- **Improve `basic` with a login page** (fetch-based Basic). Rejected: browsers still cache the Authorization header and there is still no sign-out. Changing basic's behaviour would also break scripted clients that rely on it.
- **Random in-memory signing key.** Rejected: every restart would sign everyone out, which is painful when deploying often. The optional configured session key gives the same protection without that cost.
- **Per-IP sign-in limits.** Rejected: behind a proxy the client address comes from headers a client can set (`True-Client-IP`), so a per-IP limit is easy to dodge.
- **OIDC now.** Rejected as out of scope. It needs an IdP; FB-5 tracks it.
