# Kubescope

Kubescope is a web-based Kubernetes dashboard that runs as a **single Docker container**. Point it at the kubeconfigs already on your machine, switch between contexts, and browse and operate on **every resource type** in the selected cluster — core resources and CRDs, with logs, exec, and guarded mutations. Docker-first and self-hostable anywhere: no desktop app, no in-cluster install.

## Quick start

```sh
docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest
```

Then open http://localhost:8080.

- Your kubeconfig is mounted **read-only** at `/kubeconfig` inside the container.
- **Linux:** the container runs as a non-root user, and a bind mount keeps the host file's owner and mode — a typical `0600` kubeconfig is unreadable inside. Add `--user "$(id -u):$(id -g)"` to the `docker run`. (macOS/Windows file sharing remaps ownership, so this isn't needed there.)
- **Local clusters** (kind/minikube/k3d) advertise the API server on `127.0.0.1`, which inside a container is the container itself. Add `--network host` on Linux, or rewrite the server address to `host.docker.internal` on Mac/Windows. Details and other auth gotchas (exec plugins, file-path certs): [docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md](docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md).
- **Port-forwarding** binds the forwarded pod port to `127.0.0.1` **inside** the container. To reach it from the host, publish that port too — e.g. `-p 15000:15000` — and start the forward with a matching fixed **local port** (auto-assigned ports won't be published). `--network host` (Linux) sidesteps this.

## Status

Pre-v0.1 — in active development. See [STATUS.md](STATUS.md) for the sprint board and current state.

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
| [docs/adr/](docs/adr/) | Architecture decision records (0001–0006) |

## License

Apache-2.0
