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
deploy/testenv/testenv.sh up       # create clusters + apply resources
deploy/testenv/testenv.sh run      # build + run kubescope against it (:8080)
deploy/testenv/testenv.sh status   # list clusters + workloads
deploy/testenv/testenv.sh down     # delete both clusters
deploy/testenv/testenv.sh check    # verify required tools (add --install to fix)
```

Or via Make: `make testenv-up`, `make testenv-status`, `make testenv-down`.

`up` is idempotent — re-running it re-applies the manifests and repairs the
kubeconfig without recreating existing clusters.

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
