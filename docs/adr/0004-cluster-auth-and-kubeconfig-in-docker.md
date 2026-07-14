# 0004. Cluster auth and kubeconfig in Docker

- **Status:** Accepted
- **Date:** 2026-07-14

## Context

Kubescope runs in a Docker container but must authenticate to clusters described by kubeconfigs on the **host**. A container is network-isolated and has no host tooling, so kubeconfig entries that assume host context (file paths, exec plugins, localhost API servers) break in non-obvious ways. This is the project's hardest operational problem; this ADR is its canonical documentation.

## Decision

Mount the kubeconfig **read-only** into the container at `/kubeconfig` (path configurable via `KUBESCOPE_KUBECONFIG`, falling back to `$KUBECONFIG`, then `~/.kube/config` for the bare binary). Kubescope parses it, enumerates contexts, and builds a `rest.Config` per context.

```sh
docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest
```

### Auth-method support matrix (v1)

| Kubeconfig auth style | Works in container? | What's needed |
|---|---|---|
| Embedded `certificate-authority-data` / `client-certificate-data` / token | Yes, as-is | Nothing — preferred format |
| **File-path** cert/key/CA references | Only if mounted | Mount each referenced file at the **same path** the kubeconfig names (extra `-v` flags) |
| **exec plugin** — EKS `aws eks get-token` | Not by default | CLI + cloud creds inside the container (see below) |
| **exec plugin** — GKE `gke-gcloud-auth-plugin` | Not by default | Same problem; same options |
| Static bearer token / basic auth | Yes, as-is | Nothing |

### exec-plugin stance (v1)

exec plugins spawn a host CLI that doesn't exist in the image. Documented options, chosen per user:

| Option | How | Trade-offs |
|---|---|---|
| Mount cloud creds + bundle CLI | Mount `~/.aws` (ro); build image with `aws-cli` behind a **build flag** (not in the default image) | Works transparently; image grows ~100MB+; cloud creds inside the container; per-cloud CLI sprawl |
| Pre-generated token | Run `aws eks get-token` (or equivalent) on the host; write a token-based kubeconfig variant and mount that | Default slim image works untouched; tokens expire (~15 min for EKS) — manual refresh; extra host step |

v1 ships the slim image by default and **documents** both paths. Never silently assume exec auth works; surface a clear per-context error when the plugin binary is missing.

### Local clusters (kind / minikube / k3d)

Their API server address is `127.0.0.1:<port>` — which inside a container is the container itself. First-run failure #1. Fixes:

| Host OS | Fix |
|---|---|
| Linux | `docker run --network host ...` (drop `-p`) |
| macOS / Windows | Rewrite the server address to `host.docker.internal:<port>`; the cluster's cert usually lacks that SAN, so pair with `insecure-skip-tls-verify: true` in a copy of the kubeconfig (local dev only) |

## Consequences

**Positive:**
- Zero-config happy path for embedded-cred kubeconfigs — the common case for dev/uat clusters.
- Read-only mount means Kubescope can never corrupt the user's kubeconfig.
- Per-context `rest.Config` isolates auth failures to individual contexts; other contexts keep working.

**Negative:**
- exec-plugin clusters (EKS/GKE) are not turnkey in v1; users must pick a documented workaround.
- Local-cluster networking needs a per-OS flag/rewrite; docs and error messages must carry this.
- File-path cert kubeconfigs need mount gymnastics that Kubescope cannot automate.

## Alternatives considered

- **In-cluster deployment with ServiceAccount** — rejected for v1. Abandons the "point at local kubeconfigs, switch contexts" core use case; it's a different product shape.
- **Bundle all cloud CLIs in the default image** — rejected. Bloats the image for every user to serve some; kept available behind a build flag.
- **Reimplement exec-plugin protocols natively in Go** (e.g. sign EKS tokens via SDK) — rejected for v1. Per-cloud auth code with its own credential handling and security surface; revisit if demand is proven.
