# Local test environment

A reproducible sandbox for manually exercising Kubescope against real clusters —
two [kind](https://kind.sigs.k8s.io/) clusters seeded with sample workloads.

Everything lives in an **isolated kubeconfig** (`deploy/testenv/kubeconfig`,
gitignored), so your real `~/.kube/config` is never touched.

## Requirements

- **Docker** (running) — install [Docker Desktop](https://www.docker.com/products/docker-desktop/)
- **kind** and **kubectl** — `brew install kind kubernetes-cli`, or let the
  script install them: `deploy/testenv/testenv.sh check --install`

`go` is only needed for the `run` convenience command (to build the binary).
No Helm or other tools are required.

## Usage

```sh
deploy/testenv/testenv.sh up              # create clusters + apply resources
deploy/testenv/testenv.sh run             # build + run kubescope (native) against it (:8080)
deploy/testenv/testenv.sh run --docker    # ...or run the container image instead
deploy/testenv/testenv.sh status          # list clusters + workloads
deploy/testenv/testenv.sh down            # delete both clusters
deploy/testenv/testenv.sh check           # verify required tools (add --install to fix)
```

Or via Make: `make testenv-up`, `make testenv-run`, `make testenv-run-docker`,
`make testenv-status`, `make testenv-down`.

`up` is idempotent — re-running it re-applies the manifests and repairs the
kubeconfig without recreating existing clusters.

## Running natively vs. in Docker

`run` builds and execs the native `bin/kubescope` binary against the isolated
kubeconfig — the simplest path, and the apiserver at `127.0.0.1` is reachable
directly. Pass `--build` to force a rebuild first.

`run --docker` runs the published container image instead
(`KUBESCOPE_IMAGE`, default `ghcr.io/skriptvalley/kubescope:latest`; pass
`--build` to `make docker-build` it first — otherwise the image must already be
present locally, or the script tells you to build it). The kubeconfig is copied
into a throwaway temp dir and adapted per OS so the containerised process can
reach the kind apiservers:

- **macOS / Windows** — kind advertises `127.0.0.1`, which inside a container is
  the container itself, so every cluster entry is rewritten to
  `host.docker.internal` and TLS verification is disabled (**local dev only** —
  the cluster cert does not cover that name). The image is published on
  `-p 127.0.0.1:8080:8080` — loopback only, since no auth is configured.
  - Caveat: `host.docker.internal` only resolves to a live cluster **while that
    cluster exists**. Tear the clusters down (`down`) and Kubescope will show
    its starter / "cluster unreachable" states — the connectivity flow FB-6
    adds — rather than data.
- **Linux** — `host.docker.internal` is not available, so the kubeconfig is left
  as-is and the container runs with `--network host` (Linux-only) plus
  `KUBESCOPE_LISTEN_ADDR=127.0.0.1:8080` and no `-p` — under host networking,
  container loopback is host loopback, and an unauthenticated dashboard must
  never bind a LAN interface (ADR-0005).

The credentials copy lives **only** in a `mktemp -d` directory removed by an
`EXIT` trap; the docker path deliberately does not `exec`, so the trap still
fires after the container stops.

## Launching with docker-compose

[`build/docker-compose.yml`](../../build/docker-compose.yml) is the declarative
equivalent of `run --docker` — one `kubescope` service, canonical `KUBESCOPE_*`
env, a read-only kubeconfig mount, and a **loopback-only** `127.0.0.1:8080`
publish (ADR-0005). It mounts a container-ready kubeconfig from one fixed path
(`build/.e2e-kubeconfig`) and does **no** per-OS rewriting itself — a prep step
writes that file first.

**kind flow** (macOS/Windows):

```sh
make testenv-up            # clusters + seeded workloads
make compose-config        # write the host.docker.internal-adapted kubeconfig → build/.e2e-kubeconfig
make docker-build-local    # build the image locally (GHCR is private today — FB-10)
make compose-up            # dashboard on http://127.0.0.1:8080
make compose-down          # stop + remove the adapted kubeconfig copy
```

> On **Linux**, a bridged compose can't reach a kind apiserver bound to
> `127.0.0.1` — use `make testenv-run-docker` (host networking) for kind there.
> The compose path targets kind on macOS/Windows and EKS on any OS.

**EKS flow** (opt-in, **costs money**): provision a real cluster, mint a static
token-kubeconfig into the same `build/.e2e-kubeconfig`, and launch the same
compose — see [`deploy/e2e-eks/README.md`](../e2e-eks/README.md) and
[ADR-0010](../../docs/adr/0010-e2e-eks-static-token-kubeconfig.md). The
distroless image has no `aws` CLI, so EKS exec-auth can't run in-container; the
token is minted host-side and mounted read-only. **Teardown is mandatory** —
`make e2e-eks-down`.

## What gets created

| Context | Contents |
|---|---|
| `kind-kubescope-dev` *(active)* | full set across the `web`, `data`, `batch` namespaces |
| `kind-kubescope-prod` | lighter set in `default` (so switching contexts shows different data) |

**dev** covers all seven typed workload kinds plus config/networking:

- **`web`** — `frontend` Deployment (3× nginx) + Service, `api` Deployment
  (2× busybox, logs every 2s) that consumes `frontend-config` + `api-credentials`
  via **both env (`envFrom`) and volume mounts**, those `frontend-config`
  ConfigMap + `api-credentials` Secret, and a `config-sync` Job that references
  `api-credentials`
- **`data`** — `postgres` StatefulSet (2×) + headless Service, `log-agent` DaemonSet
- **`batch`** — `db-migrate` Job (completes), `hourly-report` CronJob (**every
  minute**), `crasher` Pod (**CrashLoopBackOff** → Warning events + previous logs)

The per-minute CronJobs and the crasher keep the events feed and job lists
churning live, so live updates are visible without doing anything.

## What to try

- **Live updates** — open **web → Deployments**, then
  `kubectl --context kind-kubescope-dev -n web scale deploy/frontend --replicas=6`
  and watch rows update in place (the "Live" badge stays green).
- **Log streaming** — an `api` pod → **Logs** tab; toggle **Previous** on
  `batch/crasher`.
- **Events feed** — sidebar **Events**, filter by **Warning**.
- **Context switch** — flip to `kind-kubescope-prod` and watch every view
  repopulate.
- **Service endpoints** *(FB-13 fixture)* — the `web/frontend` Service resolves
  to **3 ready endpoint pods**; the service-level port-forward round-robins each
  new TCP connection across them (per-connection, kube-proxy semantics).
- **Resource-graph edges** *(FB-14 fixture)* — `web/api` references the
  ConfigMap + Secret via env **and** volume, and `web/config-sync` references the
  Secret, so the graph has real Pod→ConfigMap/Secret and Job→Secret edges;
  `batch/hourly-report` (every minute) is a CronJob→Jobs→pods run series to club.

To run kubectl yourself: `export KUBECONFIG=deploy/testenv/kubeconfig`.

## Customizing

Override via env vars (see the header of `testenv.sh`):
`KUBESCOPE_TESTENV_DEV_CLUSTER`, `KUBESCOPE_TESTENV_PROD_CLUSTER`,
`KUBESCOPE_TESTENV_KUBECONFIG`. Edit `manifests/dev.yaml` / `manifests/prod.yaml`
to change the seeded resources.
