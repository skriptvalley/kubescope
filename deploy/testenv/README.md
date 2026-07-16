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

## What gets created

| Context | Contents |
|---|---|
| `kind-kubescope-dev` *(active)* | full set across the `web`, `data`, `batch` namespaces |
| `kind-kubescope-prod` | lighter set in `default` (so switching contexts shows different data) |

**dev** covers all seven typed workload kinds plus config/networking:

- **`web`** — `frontend` Deployment (3× nginx) + Service, `api` Deployment
  (2× busybox, logs every 2s), `frontend-config` ConfigMap, `api-credentials` Secret
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

To run kubectl yourself: `export KUBECONFIG=deploy/testenv/kubeconfig`.

## Customizing

Override via env vars (see the header of `testenv.sh`):
`KUBESCOPE_TESTENV_DEV_CLUSTER`, `KUBESCOPE_TESTENV_PROD_CLUSTER`,
`KUBESCOPE_TESTENV_KUBECONFIG`. Edit `manifests/dev.yaml` / `manifests/prod.yaml`
to change the seeded resources.
