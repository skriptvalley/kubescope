# Opt-in EKS e2e profile

A **manual, opt-in** Terraform profile that spins up a minimal **EKS** cluster,
seeds it with the same fixtures as the kind testenv, and produces a static
token-kubeconfig that `build/docker-compose.yml` mounts — so you can exercise
Kubescope against a real cloud cluster and its exec-auth path.

kind (see [`../testenv/`](../testenv/README.md)) stays the **default** for
everyday feature work — free, local, no cloud credentials. Reach for this profile
only when you specifically want the EKS/cloud-auth smoke.

> ## ⚠️ This costs real money — teardown is mandatory
>
> `terraform apply` here creates a **billed** EKS control plane, EC2 worker
> nodes, and a NAT gateway. They keep charging until destroyed.
>
> **Run `make e2e-eks-down` when you are finished** — and confirm in the AWS
> console that nothing remains. This profile is **never** wired into `make test`
> or CI; nothing runs it automatically.

## Why a static token-kubeconfig? (ADR-0010)

An EKS kubeconfig authenticates with an exec plugin — `exec: aws eks get-token`.
The shipped Kubescope image is `distroless-static` ([ADR-0002](../../docs/adr/0002-single-binary-embedded-spa.md)):
no shell, no `aws` binary. client-go therefore **cannot run that exec plugin
inside the container** (the in-container exec-plugin problem in
[ADR-0004](../../docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md) / FB-5).

Rather than baking `aws` + credentials into the image (a fat, glibc-based fork of
the runtime), the `kubeconfig` helper mints a **short-lived bearer token on the
host** and embeds it into a kubeconfig with **no exec stanza and no AWS creds**.
The container just presents the bearer token. Full rationale:
[ADR-0010](../../docs/adr/0010-e2e-eks-static-token-kubeconfig.md).

The token TTL is **~15 min**. Past expiry the dashboard returns `401`s — re-run
`make e2e-eks-kubeconfig` to refresh it.

## Requirements (host)

- **AWS credentials** with permission to create VPC/EKS/EC2/IAM (e.g. `aws configure` / SSO). The IAM identity that runs `apply` becomes cluster-admin.
- **terraform** ≥ 1.3, **aws** CLI v2, **kubectl**, **jq**.
- **Docker** + docker-compose for the dashboard (see [`../../build/docker-compose.yml`](../../build/docker-compose.yml)).

## Flow

```sh
make e2e-eks-up            # terraform init + apply: VPC + EKS + node group, then seed the fixtures
make e2e-eks-kubeconfig    # mint the static token-kubeconfig → build/.e2e-kubeconfig (~15-min TTL)
make docker-build-local    # build the image locally (GHCR is private today — FB-10)
make compose-up            # dashboard on http://127.0.0.1:8080
# ... exercise Kubescope against the EKS cluster ...
make compose-down          # stop the dashboard
make e2e-eks-down          # terraform destroy — REQUIRED to stop billing
```

If the dashboard starts returning `401`s, the token expired — re-run
`make e2e-eks-kubeconfig` (no need to restart the cluster or the container's
next request will pick up the refreshed mount on reconnect; restart compose if it
does not).

## What it creates

| Resource | Notes |
|---|---|
| VPC (`terraform-aws-modules/vpc`) | 2 AZs, **single** NAT gateway (cost-minimal) |
| EKS cluster (`terraform-aws-modules/eks`) | public endpoint (the host/container reach it over the internet); creator gets cluster-admin via an access entry |
| Managed node group | `var.node_desired_size` × `var.node_instance_type` (default 2 × `t3.medium`) |
| Seed fixtures | the **same** `../testenv/manifests/dev.yaml` the kind testenv uses, applied via `kubectl` at apply time |

`t3.micro` is intentionally **not** the default: its EKS max-pods is 4 (ENI/IP
limit) and it has only 1 GiB RAM, leaving no room for the seed workloads after
the aws-node/kube-proxy/CoreDNS system pods. `t3.small`/`t3.medium` is the floor.

## Configuration

All AWS specifics are variables with cheap defaults (see
[`variables.tf`](variables.tf)) — `region` (default `ap-south-1`),
`cluster_name`, `cluster_version`, `node_instance_type`, `node_desired_size`,
`node_min_size`, `node_max_size`. Override with a gitignored `terraform.tfvars`
or `-var` flags, e.g.:

```sh
cd deploy/e2e-eks && terraform apply -var region=us-east-1 -var node_instance_type=t3.small
```

## Validation (no cluster, no cost)

`terraform fmt -check` and `terraform validate` verify the profile without
creating anything (run `terraform init` first for `validate`):

```sh
cd deploy/e2e-eks && terraform init -backend=false && terraform validate && terraform fmt -check
```
