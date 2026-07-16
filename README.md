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
- A **Host-header allowlist** protects localhost/loopback binds against DNS-rebinding. See [ADR-0005](docs/adr/0005-security-posture-read-only-and-secret-masking.md).

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

## Connecting to clusters

Kubescope parses the mounted kubeconfig, enumerates its contexts, and builds a connection per context. Kubeconfigs with **embedded** certificate/token data (the common dev/UAT case) work as-is. Some auth styles need extra care inside a container — full details in [ADR-0004](docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md):

- **Local clusters (kind / minikube / k3d)** advertise their API server on `127.0.0.1:<port>`, which inside a container is the container itself. On **Linux**, run with `--network host` (drop `-p`). On **macOS/Windows**, rewrite the server address to `host.docker.internal:<port>` in a copy of the kubeconfig; the cluster cert usually lacks that SAN, so pair it with `insecure-skip-tls-verify: true` (local dev only).
- **exec-plugin auth (EKS `aws eks get-token`, GKE `gke-gcloud-auth-plugin`)** spawns a host CLI that the slim image does not contain. Either bundle the CLI + mount cloud creds, or pre-generate a token on the host and mount a token-based kubeconfig (tokens expire — refresh manually). Kubescope surfaces a clear per-context error when the plugin binary is missing.
- **File-path cert/key/CA references** in a kubeconfig resolve to host paths the container can't see. Mount each referenced file at the **same path** the kubeconfig names (extra `-v` flags).
- **Port-forwarding** binds the forwarded pod port to `127.0.0.1` **inside** the container. To reach it from the host, publish that port too (e.g. `-p 15000:15000`) and start the forward with a matching fixed **local port**. `--network host` (Linux) sidesteps this.

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
| [docs/adr/](docs/adr/) | Architecture decision records (0001–0006) |

## License

Apache-2.0
