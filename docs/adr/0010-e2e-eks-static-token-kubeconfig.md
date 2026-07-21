# 0010. EKS e2e via a host-minted static token-kubeconfig (manual, opt-in)

- **Status:** Accepted
- **Date:** 2026-07-21

## Context

The e2e test harness (FB-12) exercises Kubescope against real clusters. kind covers the free/local path and the existing `deploy/testenv/` already seeds a full fixture set, so kind needs no cloud credentials and is the zero-cost default. The wrinkle is **EKS**, which we want as the cloud-auth smoke path.

An EKS kubeconfig authenticates with an **exec plugin**:

```yaml
users:
- user:
    exec:
      command: aws
      args: [eks, get-token, --cluster-name, ...]
```

The shipped image is `gcr.io/distroless/static-debian12:nonroot` ([ADR-0002](0002-single-binary-embedded-spa.md)) — no shell, no `aws` binary. client-go therefore cannot run that exec plugin **inside the container**: this is the unsolved in-container exec-plugin problem already flagged in [ADR-0004](0004-cluster-auth-and-kubeconfig-in-docker.md) (the exec-plugin stance) and FB-5. Two ways to make EKS auth work for the harness:

1. **Bake `aws` (+ shell + creds) into the image.** Forks the runtime off distroless-static onto a glibc base, grows the image ~100 MB+, and puts cloud credentials inside the container. [ADR-0004](0004-cluster-auth-and-kubeconfig-in-docker.md) already keeps this behind a build flag, not in the default image; taking it for a test harness would change the shipped artifact for everyone.
2. **Mint a static bearer token on the host and mount it.** `aws eks get-token` runs host-side (where `aws` and creds already live), and the resulting short-lived token is embedded into a kubeconfig with no exec stanza. The container authenticates with a plain bearer token — no `aws`, no AWS creds, no image change.

## Decision

The EKS profile (`deploy/e2e-eks/`) mints a **static bearer-token kubeconfig host-side** and mounts it read-only into the container — option 2.

- Terraform (`terraform-aws-modules` VPC + EKS) provisions one minimal cluster and outputs the API `endpoint` + cluster `certificate-authority-data`.
- A `kubeconfig` helper (`deploy/e2e-eks/kubeconfig.sh`, `make e2e-eks-kubeconfig`) runs `aws eks get-token` on the host and writes a kubeconfig — endpoint + CA from the Terraform outputs, `token:` embedded — **with no `exec` stanza and no AWS credentials**. It writes to the fixed path `docker-compose` mounts (`build/.e2e-kubeconfig`), the same slot the kind flow uses.
- The **shipped image is unchanged**: still distroless-static, still no `aws`, still the canonical `KUBESCOPE_*` env set. The token-kubeconfig is a host-side test artifact, never baked in.
- **TTL trade-off:** the EKS token is short-lived (~15 min). Past expiry the dashboard gets `401`s; the fix is to re-run `make e2e-eks-kubeconfig` (documented in the helper output and the README). This is acceptable for a scripted smoke; it is **not** a production auth story.

### Posture: manual, opt-in, never in CI

An EKS cluster costs real ₹ (control plane + EC2 + NAT gateway). The whole profile is therefore **manual and opt-in**:

- It is **never** wired into `make test` or CI — CI runs only `terraform fmt -check` + `terraform validate` (no `apply`).
- **Teardown is mandatory and loud.** `make e2e-eks-down` maps to `terraform destroy`; a prominent cost + teardown warning appears in the README and in a Terraform output surfaced after `apply`.
- kind stays the default for everyday feature testing (FB-13 service port-forward, FB-14 resource graph) — no cloud required.

Loopback-only exposure is unchanged from [ADR-0005](0005-security-posture-read-only-and-secret-masking.md): `docker-compose` publishes `127.0.0.1:8080:8080`, never a LAN interface, whichever cluster backs it.

## Consequences

**Positive:**
- The default image never grows an `aws`/glibc dependency and never carries cloud credentials — EKS auth is solved entirely host-side.
- The same `docker-compose` consumes either a kind- or EKS-adapted kubeconfig from one fixed path; the compose does no per-OS or per-cloud rewriting.
- `terraform fmt`/`validate` are the only CI touchpoints, so the billed path can never fire automatically.

**Negative:**
- The EKS token expires (~15 min); long sessions must re-mint. A deliberate trade for keeping the image and container credential-free.
- The harness assumes `aws`, `kubectl`, and `terraform` on the host — reasonable for an opt-in cloud smoke, unnecessary for the kind default.
- A forgotten `e2e-eks-down` bills continuously; mitigated by warnings, an `Ephemeral` tag on every resource, and the mandatory-teardown callout — not by anything that can force cleanup.

## Alternatives considered

- **Bundle `aws` + creds into the image** — rejected. Forks off distroless-static, bloats the artifact for every user, and puts cloud creds in the container; [ADR-0004](0004-cluster-auth-and-kubeconfig-in-docker.md) already keeps CLI-in-image behind a build flag.
- **Reimplement the EKS token signer in Go** (SDK-sign the STS `GetCallerIdentity` presigned URL natively) — rejected for a test harness. It is exactly the per-cloud auth-code path [ADR-0004](0004-cluster-auth-and-kubeconfig-in-docker.md) defers until demand is proven; the host-mint helper needs no new Go code or dependency.
- **Terraform `kubernetes_manifest` for the seed fixtures** — rejected. It requires the cluster reachable at plan time and a configured kubernetes provider, which would make `terraform validate` need a live cluster. The seed runs via a `null_resource` `local-exec` (`kubectl apply` of the Story A manifests) so `validate` stays infra-only.
- **A second, EKS-specific compose file** — rejected. One compose mounting a fixed path keeps the kind and EKS flows identical downstream; only the host-side prep step differs.
