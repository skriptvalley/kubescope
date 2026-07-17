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
| `KUBESCOPE_KUBECONFIG` | `/kubeconfig` if present, else `$KUBECONFIG`, else `~/.kube/config` | Kubeconfig **source list**: colon-separated paths, each a file **or a directory** of kubeconfig files, merged with kubectl precedence (first occurrence of a name wins). A single path behaves as before. See [ADR-0008](docs/adr/0008-kubeconfig-source-registry.md). |
| `KUBESCOPE_READ_ONLY` | `false` | When `true`, rejects all mutating operations server-side. |
| `KUBESCOPE_AUTH_MODE` | `none` | `none` \| `basic` \| `oidc` (see Authentication). |
| `KUBESCOPE_AUTH_BASIC_USERNAME` | — | Basic-auth username. Required when `KUBESCOPE_AUTH_MODE=basic`. |
| `KUBESCOPE_AUTH_BASIC_PASSWORD` | — | Basic-auth password. Required when `KUBESCOPE_AUTH_MODE=basic`. Never logged. |
| `KUBESCOPE_ALLOW_KUBECONFIG_SET` | `false` | When `true`, enables the kubeconfig **source registry** endpoints (`POST`/`DELETE /api/v1/kubeconfigs`) so the UI can add/remove kubeconfig sources — files or directories — at runtime (paths must be readable by the process — in Docker, under a mounted volume). Always rejected in read-only mode; changes are in-memory and a restart reverts to `KUBESCOPE_KUBECONFIG`. See [ADR-0008](docs/adr/0008-kubeconfig-source-registry.md). |

## Connecting to clusters

Kubescope parses the mounted kubeconfig(s), enumerates their contexts, and builds a connection per context. Kubeconfigs with **embedded** certificate/token data (the common dev/UAT case) work as-is. Some auth styles need extra care inside a container — full details in [ADR-0004](docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md):

- **Local clusters (kind / minikube / k3d)** advertise their API server on `127.0.0.1:<port>`, which inside a container is the container itself. On **Linux**, run with `--network host` (drop `-p`). On **macOS/Windows**, rewrite the server address to `host.docker.internal:<port>` in a copy of the kubeconfig; the cluster cert usually lacks that SAN, so pair it with `insecure-skip-tls-verify: true` (local dev only).
- **exec-plugin auth (EKS `aws eks get-token`, GKE `gke-gcloud-auth-plugin`)** spawns a host CLI that the slim image does not contain. Either bundle the CLI + mount cloud creds, or pre-generate a token on the host and mount a token-based kubeconfig (tokens expire — refresh manually). Kubescope surfaces a clear per-context error when the plugin binary is missing.
- **File-path cert/key/CA references** in a kubeconfig resolve to host paths the container can't see. Mount each referenced file at the **same path** the kubeconfig names (extra `-v` flags).
- **Port-forwarding** binds the forwarded pod port to `127.0.0.1` **inside** the container. To reach it from the host, publish that port too (e.g. `-p 15000:15000`) and start the forward with a matching fixed **local port**. `--network host` (Linux) sidesteps this.

### First run and failure states

Kubescope never dead-ends on a cluster problem. With no kubeconfig (or an empty/broken one) it starts anyway and shows a **guided setup page** instead of an error, and every connectivity failure is classified — connection refused, DNS, TLS, missing exec plugin, expired auth, RBAC denial, timeout, API-server error — with an inline fix suggestion and a doc link at the point of failure. If the active cluster goes away while you're viewing it (e.g. `kind delete cluster`), live views show an unreachable banner, polling backs off, and everything resumes automatically when the cluster returns; switching to a healthy context always works in the meantime.

### Multiple kubeconfigs and adding clusters at runtime

If you keep one kubeconfig file per cluster instead of a single merged file, mount the whole directory once and point Kubescope at it:

```sh
docker run --rm -p 8080:8080 -v ~/.kube:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest
```

Every parseable kubeconfig file in the directory is loaded and merged with kubectl semantics (first occurrence of a context/cluster/user name wins; broken or oversized files are skipped and reported per file, never as a global failure). `KUBESCOPE_KUBECONFIG` also accepts an explicit colon-separated list mixing files and directories, e.g. `-e KUBESCOPE_KUBECONFIG=/kubeconfigs:/extra/staging.yaml`.

Because directories are re-scanned on every request, **dropping a new file into a mounted directory registers its clusters without a restart** — this is the supported way to add a cluster to a running container, since Docker has no runtime mounts. The UI's *Manage kubeconfig sources* surface (context-switcher menu, or the setup page before a connection) lists every source with per-file status and shadowed names, and offers a rescan.

To add or remove **sources** from the UI at runtime, start with `KUBESCOPE_ALLOW_KUBECONFIG_SET=true` (see Configuration). Runtime changes are in-memory only — a restart reverts to the configured baseline. Details in [ADR-0008](docs/adr/0008-kubeconfig-source-registry.md).

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
| [docs/adr/](docs/adr/) | Architecture decision records (0001–0008) |

## License

Apache-2.0
