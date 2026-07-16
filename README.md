# Kubescope

Kubescope is a web-based Kubernetes dashboard that runs as a **single Docker container**. Point it at the kubeconfigs already on your machine, switch between contexts, and browse and operate on **every resource type** in the selected cluster — core resources and CRDs, with logs, exec, and guarded mutations. Docker-first and self-hostable anywhere: no desktop app, no in-cluster install.

## Quick start

```sh
docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest
```

Then open http://localhost:8080. Pin a specific release with a version tag, e.g. `ghcr.io/skriptvalley/kubescope:0.1.0`.

- Your kubeconfig is mounted **read-only** at `/kubeconfig` inside the container.
- **Linux:** the container runs as a non-root user, and a bind mount keeps the host file's owner and mode — a typical `0600` kubeconfig is unreadable inside. Add `--user "$(id -u):$(id -g)"` to the `docker run`. (macOS/Windows file sharing remaps ownership, so this isn't needed there.)

## ⚠️ Security & network exposure

Kubescope acts with the **full power of whatever credentials your kubeconfig carries** — it can read Secrets, exec into pods, and mutate or delete any resource. Treat a running instance as a cluster-admin console.

- **Do not expose Kubescope publicly without both** authentication (`KUBESCOPE_AUTH_MODE=basic`) **and** network controls (firewall, VPN, or an authenticating reverse proxy). The safest posture is localhost or a trusted network only.
- The **bare binary binds `127.0.0.1:8080`** (localhost only) by default. The **Docker image binds `0.0.0.0:8080`** because the container's network namespace is the isolation boundary; the published port (`-p 8080:8080`) is what makes it reachable, so scope that publish carefully (e.g. `-p 127.0.0.1:8080:8080` to keep it on the host's loopback).
- **Read-only mode** (`KUBESCOPE_READ_ONLY=true`) rejects **all** mutating operations server-side — a good default for shared or demo instances.
- **Secret values** are masked by default in every view; revealing a value is an explicit per-key action. Secret data is never written to logs.
- A **Host-header allowlist** protects localhost/loopback binds against DNS-rebinding: requests whose `Host` is neither a loopback name nor the configured bind address are rejected. If you front the loopback binary with a **reverse proxy**, configure the proxy to send `Host: localhost` (e.g. nginx `proxy_set_header Host localhost;`) or bind Kubescope to a concrete address the proxy targets — otherwise the proxied public hostname is rejected. Wildcard binds (`0.0.0.0`, the image default) skip this check. See [ADR-0005](docs/adr/0005-security-posture-read-only-and-secret-masking.md).

See [ADR-0005](docs/adr/0005-security-posture-read-only-and-secret-masking.md) for the full security posture.

## Authentication

Auth is selected by `KUBESCOPE_AUTH_MODE`:

| Mode | Behavior |
|---|---|
| `none` (default) | No authentication. Only appropriate on a trusted/loopback network. |
| `basic` | HTTP Basic auth gates every route except `/healthz`. Requires `KUBESCOPE_AUTH_BASIC_USERNAME` and `KUBESCOPE_AUTH_BASIC_PASSWORD`; the server refuses to start if either is missing. |
| `oidc` | Reserved; **not implemented** in this release — selecting it fails fast at startup. |

Enable Basic auth:

```sh
docker run --rm -p 8080:8080 \
  -v ~/.kube/config:/kubeconfig:ro \
  -e KUBESCOPE_AUTH_MODE=basic \
  -e KUBESCOPE_AUTH_BASIC_USERNAME=admin \
  -e KUBESCOPE_AUTH_BASIC_PASSWORD='choose-a-strong-password' \
  ghcr.io/skriptvalley/kubescope:latest
```

The browser prompts for credentials on first load. The password lives in the process environment, so treat that environment as sensitive (hashed/file-based credentials and OIDC are planned for a later release).

## Configuration

All configuration is via `KUBESCOPE_`-prefixed environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `KUBESCOPE_LISTEN_ADDR` | `127.0.0.1:8080` (binary); `0.0.0.0:8080` (image) | `host:port` to bind. |
| `KUBESCOPE_PORT` | — | Overrides only the **port** part of the listen address. |
| `KUBESCOPE_KUBECONFIG` | `/kubeconfig` if present, else `$KUBECONFIG`, else `~/.kube/config` | Path to the kubeconfig to load. |
| `KUBESCOPE_READ_ONLY` | `false` | When `true`, rejects all mutating operations server-side. |
| `KUBESCOPE_AUTH_MODE` | `none` | `none` \| `basic` \| `oidc` (see Authentication). |
| `KUBESCOPE_AUTH_BASIC_USERNAME` | — | Basic-auth username. Required when `KUBESCOPE_AUTH_MODE=basic`. |
| `KUBESCOPE_AUTH_BASIC_PASSWORD` | — | Basic-auth password. Required when `KUBESCOPE_AUTH_MODE=basic`. Never logged. |
| `KUBESCOPE_ALLOW_KUBECONFIG_SET` | `false` | When `true`, enables `PUT /api/v1/kubeconfig` so the UI can repoint Kubescope at another kubeconfig **path** at runtime (path must be readable by the process — in Docker, a mounted volume). Always rejected in read-only mode; the override is in-memory and a restart reverts to the configured path. See [ADR-0007](docs/adr/0007-runtime-kubeconfig-source.md). |

## Connecting to clusters

Kubescope parses the mounted kubeconfig, enumerates its contexts, and builds a connection per context. Kubeconfigs with **embedded** certificate/token data (the common dev/UAT case) work as-is. Some auth styles need extra care inside a container — full details in [ADR-0004](docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md):

- **Local clusters (kind / minikube / k3d)** advertise their API server on `127.0.0.1:<port>`, which inside a container is the container itself. On **Linux**, run with `--network host` (drop `-p`). On **macOS/Windows**, rewrite the server address to `host.docker.internal:<port>` in a copy of the kubeconfig; the cluster cert usually lacks that SAN, so pair it with `insecure-skip-tls-verify: true` (local dev only).
- **exec-plugin auth (EKS `aws eks get-token`, GKE `gke-gcloud-auth-plugin`)** spawns a host CLI that the slim image does not contain. Either bundle the CLI + mount cloud creds, or pre-generate a token on the host and mount a token-based kubeconfig (tokens expire — refresh manually). Kubescope surfaces a clear per-context error when the plugin binary is missing.
- **File-path cert/key/CA references** in a kubeconfig resolve to host paths the container can't see. Mount each referenced file at the **same path** the kubeconfig names (extra `-v` flags).
- **Port-forwarding** binds the forwarded pod port to `127.0.0.1` **inside** the container. To reach it from the host, publish that port too (e.g. `-p 15000:15000`) and start the forward with a matching fixed **local port**. `--network host` (Linux) sidesteps this.

### First run and failure states

Kubescope never dead-ends on a cluster problem. With no kubeconfig (or an empty/broken one) it starts anyway and shows a **guided setup page** instead of an error, and every connectivity failure is classified — connection refused, DNS, TLS, missing exec plugin, expired auth, RBAC denial, timeout, API-server error — with an inline fix suggestion and a doc link at the point of failure. If the active cluster goes away while you're viewing it (e.g. `kind delete cluster`), live views show an unreachable banner, polling backs off, and everything resumes automatically when the cluster returns; switching to a healthy context always works in the meantime.

To point a running instance at a different kubeconfig from the UI, start it with `KUBESCOPE_ALLOW_KUBECONFIG_SET=true` (see Configuration).

## Status

**v0.1.0** — first tagged release; see [CHANGELOG.md](CHANGELOG.md) for what landed across sprints 0–8 and [STATUS.md](STATUS.md) for the sprint board.

## Local development

Prereqs: Go 1.23+, Node 20+.

```sh
make dev    # run backend + frontend in dev mode
make test   # run test suites
```

Run `make help` for all targets (build, lint, docker-build, kind-up, smoke, …). Full contributor guide: [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md).

## Documentation

| Doc | Contents |
|---|---|
| [docs/PRD.md](docs/PRD.md) | Problem, v1 scope, non-goals, success criteria |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System design, components, data flows |
| [docs/BUILD-PLAN.md](docs/BUILD-PLAN.md) | Sprint plan (0–8) and v2 backlog |
| [CHANGELOG.md](CHANGELOG.md) | Release notes |
| [docs/adr/](docs/adr/) | Architecture decision records (0001–0007) |

## License

Apache-2.0
