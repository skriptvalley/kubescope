# 0012. Sub-path serving behind a reverse proxy (`KUBESCOPE_BASE_PATH`)

- **Status:** Accepted
- **Date:** 2026-09-24

## Context

Kubescope assumed it owned the root of its origin: Vite emitted `/assets/…`, every API/SSE/WebSocket URL was root-absolute (`/api/v1/…`), and the router had no basename. That is fine for `docker run -p 8080:8080`, but it breaks the common "one host, many tools" shape — e.g. `https://projects.example.dev/kubescope/` behind Traefik or nginx, next to other apps on the same host. With a path-stripping proxy the shell loads, then `/assets/…` and `/api/…` hit whatever owns the host root.

The concrete driver is running Kubescope on a single-node k3s cluster behind Traefik, gated by an existing console's session through ForwardAuth, at `/kubescope/`. The shape generalises to any reverse proxy.

Constraints: one image must serve both at the root and under a prefix (ADR-0002, single embedded build); no trust in client-supplied headers for anything written into HTML; and proxies differ — Traefik `stripPrefix` removes the prefix, a plain nginx `proxy_pass` often keeps it.

## Decision

A new canonical env var, **`KUBESCOPE_BASE_PATH`** (default unset = root), names the sub-path, e.g. `/kubescope`.

- **Config** (`internal/config`) normalizes it to `""` or `/seg[/seg…]` (leading slash, no trailing slash). Segments are limited to URL-unreserved characters, `.`/`..` are rejected, and a first segment that would shadow a Kubescope route (`api`, `healthz`, `assets`) is refused. Startup fails loudly on a bad value.
- **Build**: Vite `base: "./"` — every emitted asset URL (entry script, CSS, fonts, the lazily loaded graph chunk) is relative.
- **Server** (`internal/server`): `serveIndex` writes `<base href="<BASE_PATH>/">` as the first element of `<head>`. It always writes one, `/` at the root, because a relative asset on a deep link like `/resources/core/v1/pods` would otherwise resolve under that route. A thin wrapper (`stripBasePath`) removes the prefix when the request still carries it, so Kubescope works whether or not the proxy strips. The value is validated config, never a request header.
- **SPA** (`web/src/lib/base.ts`): `basePath` is read once from the `<base href>`. `request()` (fetch), `openStream()` (EventSource) and the exec WebSocket URL go through `withBase()`. The router's `basename` is the mount only when the page was loaded under it (`routerBasename`), so a port-forward that bypasses the proxy still renders. With no `<base>` (Vite dev server, tests) everything stays at the root.
- **CSRF guard**: behind a proxy the credential is ambient (the proxy's session cookie), and several mutations are body-less POSTs a plain form could submit. `crossOriginGuard` (Go's `http.CrossOriginProtection`: `Sec-Fetch-Site`, else Origin vs Host) refuses non-safe cross-origin browser requests. It also covers cached Basic credentials when Kubescope runs standalone.

Running **in-cluster** needs no product change and stays within [ADR-0004](0004-cluster-auth-and-kubeconfig-in-docker.md)'s kubeconfig model. Mount a kubeconfig whose user is `tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token`, with `certificate-authority: …/ca.crt` and server `https://kubernetes.default.svc`. client-go re-reads the projected token as the kubelet rotates it. Authentication in front of Kubescope (`AUTH_MODE=none` behind an authenticating proxy) and network isolation are the deployment's job, per [ADR-0005](0005-security-posture-read-only-and-secret-masking.md).

## Consequences

**Positive:**
- One image serves at the root and under any sub-path, with no rebuild and no header trust.
- Works with stripping (Traefik `stripPrefix`) and non-stripping proxies alike; `/healthz` answers at both `/healthz` and `<base>/healthz`.
- The change is confined to four URL touch points plus the router; components and hooks are untouched.

**Negative:**
- One more env var in the canonical set.
- The shell always carries a `<base>` element, so any future root-relative URL in the SPA (`"/api/…"` passed straight to `fetch`) must go through `withBase()` or it will escape the mount. The API module remains the single HTTP entry point (CLAUDE.md), which contains the risk.
- Request logs show the path after the prefix is stripped.
- **A path prefix is not a browser security boundary.** Every app on the same host shares one origin, so script running in any of them (e.g. an XSS) can call Kubescope with the user's session. The CSRF guard stops other origins, including sibling subdomains, but cannot stop the same origin. A write-enabled Kubescope belongs on its own hostname; a shared-host sub-path is a convenience trade the deployment makes knowingly.

## Alternatives considered

- **Derive the prefix from `X-Forwarded-Prefix`** (which Traefik `stripPrefix` sets) — rejected as the only mechanism. It trusts a client-controllable header for a value written into HTML, and not every proxy sends it. An explicit, validated setting is simpler to reason about.
- **Rewrite absolute asset URLs in `index.html` at serve time** (keep Vite `base: "/"`) — rejected. Lazily loaded chunks and CSS `url()`s are emitted inside JS/CSS, so string-rewriting only the shell misses them.
- **Build-time base per deployment** (`vite build --base /kubescope/`) — rejected. It breaks the single published image (ADR-0002) and needs a rebuild per mount point.
- **Native in-cluster config (`rest.InClusterConfig`) as a synthetic context** — deferred. The tokenFile kubeconfig achieves the same with zero code, and FB-4's in-cluster product shape (Helm chart, ServiceAccount UX) stays a separate decision.
