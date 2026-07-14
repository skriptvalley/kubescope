# Kubescope — PRD (v1)

## Problem

Inspecting and operating on Kubernetes clusters from a laptop means juggling `kubectl` incantations, desktop apps (Lens, Headlamp desktop), or heavy in-cluster platforms. There is no lightweight, self-hostable, browser-based dashboard you can run with **one Docker command** against the kubeconfigs already on your machine — and that covers **every** resource type, CRDs included.

## Primary user

A backend/platform engineer with multiple cluster kubeconfigs locally (dev, uat, pre-prod, …) who wants a fast, browser-based way to inspect and operate on them.

## v1 scope — user stories

### Setup & contexts
- As a developer, I can run one Docker command with my kubeconfig mounted read-only and open the dashboard at `http://localhost:8080`:
  ```sh
  docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest
  ```
- As a developer, I can see every context in my kubeconfig with its connection/health status and switch the active context instantly.

### Browse & inspect
- As a developer, I can browse **every** resource type in the cluster — core APIs and CRDs, namespaced and cluster-scoped — with a namespace selector.
- As a developer, I can open any resource's detail view and its raw YAML.
- As a developer, I get deep workload views (Pods, Deployments, StatefulSets, DaemonSets, ReplicaSets, Jobs, CronJobs) with status, conditions, owned pods, and related events.

### Live data
- As a developer, I see resource lists and details update live as the cluster changes (no manual refresh).
- As a developer, I can stream pod logs live (follow, container select, previous, tail lines) and view an events feed (cluster-wide and per-namespace).

### Operate
- As a developer, I can mutate resources — edit/apply YAML, scale, rollout-restart, delete, cordon/drain — always behind typed confirmation dialogs.
- As a developer, I can exec into a pod with an in-browser terminal and start/stop port-forwards.

### Safety
- As a developer, I can run Kubescope with `KUBESCOPE_READ_ONLY=true` so every mutating operation is rejected server-side, and Secret values are masked by default (reveal-on-click).

## Non-goals (v1)

- Metrics/charts
- Multi-cluster simultaneous view
- Cost analytics
- Alerting
- Log aggregation
- A plugin system

## v2 backlog

- Resource graph (ownerReferences + selectors + config/secret/volume refs → interactive graph)
- Metrics via metrics-server (CPU/mem on pods/nodes)
- Side-by-side multi-cluster view
- Plugin/extension system

## Success criteria

| # | Criterion | Target |
|---|---|---|
| 1 | Pull image → view any resource in a mounted-kubeconfig cluster | < 2 min |
| 2 | Context switch (select → usable resource list) | < 5 s |
| 3 | CRD coverage | Any CRD installed in the cluster is browsable with no code change |
| 4 | Read-only mode | Blocks 100% of write operations (enforced server-side) |
| 5 | Secret safety | Secret values masked by default in UI; never written to logs |
| 6 | Distribution | Single multi-arch image (amd64 + arm64), single container, single process |
