# Feedback — E2E test harness (kind-first + opt-in EKS/Terraform + docker-compose)

> Feedback-driven prompt (FB-12), the first v2 item. Not part of the canonical Sprint 0–8 plan.
> Treat as a self-contained mini-sprint. Branch `fix/e2e-harness`.
> One session; do not pull FB-13/FB-14 work forward.

## Context recap
Read before starting (in order):
1. `STATUS.md` — FB-12/13/14 scope the v2 patch; this is #1 of 3, and its fixtures are dogfood targets for FB-13 (service port-forward) and FB-14 (resource graph).
2. `deploy/testenv/` — **already exists and already seeds what we need**: `testenv.sh` (`up`/`down`/`status`/`run`/`run --docker`), two kind clusters, and `manifests/dev.yaml` (web: `frontend` Deploy 3× + Service, `api` Deploy 2×, ConfigMap, Secret; data: `postgres` StatefulSet + headless Service, DaemonSet; batch: Job, per-minute CronJob, crasher Pod) + `manifests/prod.yaml`. Reuse it — do **not** reinvent kind bring-up or seeding.
3. ADRs: `0002` (single distroless binary — the reason EKS exec-auth can't run in-container), `0004` (kubeconfig-in-Docker), `0005` (security posture: loopback bind, never a LAN interface). Write the new ADR (0010) first.
4. `build/Dockerfile` — runtime is `gcr.io/distroless/static-debian12:nonroot`: no shell, no `aws` CLI. `CLAUDE.md` env table — the canonical `KUBESCOPE_*` set; **do not invent new binary env vars** (tooling/script vars like `KUBESCOPE_IMAGE` are fine).

## Why (the trigger)
We want a repeatable way to exercise Kubescope against real clusters — kind for free/local, and a real **EKS** cluster (via Terraform) for the cloud-auth path — plus a declarative `docker-compose` to launch the dashboard with the right env/creds. Constraints (recorded so they are not re-litigated):
- kind is the default: free, no cloud creds, and `deploy/testenv/` already delivers the two-cluster, fully-seeded environment. Feature testing for FB-13/FB-14 needs no cloud at all.
- **EKS auth is the wrinkle.** An EKS kubeconfig authenticates via `exec: aws eks get-token`. The shipped image is distroless-static — no `aws` binary, no shell — so client-go cannot run that exec plugin in-container (the unsolved in-container exec-plugin problem already flagged in FB-5). Baking `aws` into the image would fork the runtime off a glibc base — rejected. Instead, mint a **static bearer-token kubeconfig** host-side and mount it read-only; the container needs no AWS creds and the shipped image is unchanged. Token TTL ~15 min → fine for a scripted run; the profile documents re-minting.
- This harness is **manual and opt-in**. EKS costs real ₹ (control plane + EC2). It never runs in `make test` or CI; teardown is mandatory and loud.

## Goal
`docker-compose` launches Kubescope against a mounted, pre-adapted kubeconfig with the canonical env vars; kind stays the zero-cost default via the existing `deploy/testenv/`; and an opt-in `deploy/e2e-eks/` Terraform profile spins up a minimal EKS cluster, seeds the same fixtures, and produces a static token-kubeconfig the same compose consumes — with mandatory teardown and cost warnings.

## Stories

### Story A — Seed-fixture gaps for the graph / service port-forward features
`manifests/dev.yaml` already fronts a 3× Deployment with a Service and defines a Secret/ConfigMap. Fill only the gaps FB-13/FB-14 will assert on.
**Acceptance criteria:**
- [ ] The `api-credentials` Secret and `frontend-config` ConfigMap are **wired into a workload** — today they are standalone objects that no workload references, so no Pod→Secret/ConfigMap edge exists to draw. Add `envFrom` **and** a volume mount on a Deployment, plus the Secret referenced by a Job, so the graph has real Pod→Secret/ConfigMap and Job→Secret edges and the "club a series of runs" case (CronJob→Jobs→pods) is exercisable.
- [ ] The `frontend` Service resolves to ≥3 ready endpoint pods (already true) — the fixture for FB-13's per-connection balancing across endpoints; call it out in the testenv README's "What to try".
- [ ] Zero regression to existing testenv behavior (`testenv.sh up` stays idempotent; native `run` and `run --docker` unchanged). New manifest objects only.

### Story B — docker-compose (the declarative launch)
Add `build/docker-compose.yml` (the repo map already anticipates a compose in `build/`). It is the declarative equivalent of `testenv.sh run --docker`.
**Acceptance criteria:**
- [ ] One `kubescope` service: the image (`KUBESCOPE_IMAGE`, default `ghcr.io/skriptvalley/kubescope:latest`), a **read-only** kubeconfig mount, canonical env only (`KUBESCOPE_KUBECONFIG`, `KUBESCOPE_LISTEN_ADDR`, `KUBESCOPE_READ_ONLY`, optional `KUBESCOPE_AUTH_MODE`), and a **loopback-only** publish (`127.0.0.1:8080:8080`) — never a LAN bind (ADR-0005).
- [ ] It consumes a kubeconfig at a fixed path produced by a prep step (kind: the `host.docker.internal`-adapted copy `testenv.sh` already makes; EKS: Story C's token-kubeconfig). The compose itself does no per-OS rewriting — it mounts what the prep step wrote. Document the two prep commands.
- [ ] `make compose-up` / `make compose-down` wrap it; README documents both the kind and EKS flows.

### Story C — Opt-in EKS/Terraform profile
Add `deploy/e2e-eks/` — Terraform + a token-kubeconfig helper. Opt-in, manual, cost-warned.
**Acceptance criteria:**
- [ ] Terraform (via `terraform-aws-modules` VPC + EKS) creates one **minimal** cluster: a managed node group of the smallest viable instance (t3.small/medium — t3.micro's EKS max-pods is 4 (ENI/IP limit) and it has only 1 GiB RAM, so the aws-node/kube-proxy/CoreDNS system pods leave no room for the seed workloads), sized for the seed set. All AWS specifics (region, instance type, node count) are variables with cheap defaults.
- [ ] The same fixtures (Story A manifests) are applied once the cluster is ready.
- [ ] A `kubeconfig` helper mints a **static bearer-token kubeconfig** (`aws eks get-token` → embedded token; endpoint + CA from Terraform outputs) at the fixed path the compose mounts — **no exec stanza, no AWS creds in the container**. It documents the ~15-min TTL and the re-mint command.
- [ ] `make e2e-eks-up` / `e2e-eks-kubeconfig` / `e2e-eks-down`; `down` maps to `terraform destroy`. A prominent cost + **mandatory teardown** warning in the README and the plan output. Explicitly **not** wired into `make test` or CI.

### Story D — ADR + docs
**Acceptance criteria:**
- [ ] New ADR (0010): the token-kubeconfig decision (distroless image has no exec-plugin runtime; static bearer token over a fat test image; ~15-min TTL trade-off) and the manual/opt-in, never-in-CI posture. ADR index + `STATUS.md` updated.
- [ ] `deploy/testenv/README.md` (and/or a new `deploy/e2e-eks/README.md`) documents: the kind flow (`testenv up` → compose), the EKS flow (`e2e-eks-up` → `e2e-eks-kubeconfig` → compose → `e2e-eks-down`), the exec-auth rationale, and cost/teardown.

## Scenario matrix
| Scenario | Expected |
|---|---|
| `testenv up` → `make compose-up` (kind) | Dashboard at `127.0.0.1:8080`; both contexts + seeded workloads |
| `compose-down` | Container stopped; adapted-kubeconfig temp copy cleaned |
| `e2e-eks-up` (opt-in) | Minimal EKS cluster + seed fixtures applied |
| `e2e-eks-kubeconfig` | Static token-kubeconfig written; no exec stanza, no creds in container |
| `make compose-up` (EKS kubeconfig) | Dashboard serves against EKS overview + workloads |
| `e2e-eks-down` | `terraform destroy`; no residual EKS/EC2 in the console |
| Token past ~15-min TTL | 401 → re-run `e2e-eks-kubeconfig` (documented) |

## Testing requirements
- Terraform: `terraform validate` + `fmt -check` in the profile dir (no apply in CI).
- Manual kind smoke: `testenv up` → `make compose-up` → dashboard shows both contexts + seeded workloads; `compose-down` clean.
- Manual EKS smoke (opt-in, owner-run once): `e2e-eks-up` → `e2e-eks-kubeconfig` → `compose-up` serves against EKS → `e2e-eks-down` destroys everything (verify no residual resources).
- No new Go code ⇒ no new Go unit tests; `make test`/`lint`/`fe-test` must still pass unchanged (nothing in the build/test path changed).

## Definition of Done
- `make compose-up` works against the kind testenv; `terraform validate` clean; the EKS flow is documented and (owner) smoke-verified with teardown confirmed.
- ADR-0010 written; READMEs updated; `STATUS.md` updated (FB-12 done, ADR recorded, FB-13/FB-14 still open).
- No change to the shipped image, the canonical `KUBESCOPE_*` env set, or the CI/test path.

## End-of-session actions
1. `make test`, `make lint`, `make fe-test` (regression-only — nothing should change) + the manual kind compose smoke.
2. Update `STATUS.md` (last work `[feedback]`, next expected, FB-12 checkbox, ADR-0010).
3. Commit (Conventional Commits), push branch `fix/e2e-harness`, open PR; agent-review the diff and fix findings.
4. Gates green → squash-merge, sync `main`; concise summary.
