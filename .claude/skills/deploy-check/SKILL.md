---
name: deploy-check
description: Pre-release verification runbook — multi-arch image build, kind smoke test with mounted kubeconfig, guardrail checks, STATUS.md report.
---

# Deploy check

Run before tagging any release, and after changes to the Dockerfile, auth, streaming, or guardrail paths.

## 1. Build the multi-arch image

```sh
make docker-build                                   # local-arch image via build/Dockerfile
docker buildx build --platform linux/amd64,linux/arm64 -f build/Dockerfile .
```

- [ ] Both `amd64` and `arm64` build clean (buildx cannot `--load` multi-platform; local smoke uses the native-arch image).

## 2. Create the kind cluster

```sh
make kind-up
```

- [ ] `kubectl get nodes` works from the host against the kind context.
- [ ] Apply a sample CRD + one instance so step 4 can verify CRD browsing.
- [ ] Create a sample Secret in the default namespace for the masking check.

## 3. Run the container with the mounted kubeconfig

```sh
docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest
```

**kind-in-docker caveat (ADR-0004, `docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md`):** kind's API server address in the kubeconfig is `127.0.0.1:<port>`, which inside the container is the container itself.

- Linux: add `--network host`.
- Mac/Win: mount a kubeconfig copy with the server rewritten to `https://host.docker.internal:<port>` (TLS name caveats per ADR-0004).

## 4. Smoke checklist

| # | Check | Pass condition |
|---|---|---|
| 1 | Health | `curl localhost:8080/healthz` returns 200 |
| 2 | Contexts | All kubeconfig contexts listed in the UI |
| 3 | Context switch | Switch to the kind context; cluster overview loads |
| 4 | Generic list | Core resource types list; the sample CRD type lists too |
| 5 | Workload detail + logs | Pod detail renders; logs stream live |
| 6 | Mutation (scratch ns) | Create ns `kubescope-smoke`; in it via the UI: create a Deployment, scale it, delete it — all succeed. Then delete the ns |
| 7 | Read-only mode | Restart the container with `-e KUBESCOPE_READ_ONLY=true`; the same mutation is rejected server-side and the UI disables write controls |
| 8 | Secret masking | Secret values masked by default, reveal-on-click works, values absent from container logs |

Cleanup: delete `kubescope-smoke`, remove the sample CRD, `make kind-down` (or leave the cluster for dev).

## 5. Report into STATUS.md

- **All pass:** note `deploy-check PASS <date>, image <tag>` in the STATUS.md Summary for the session.
- **Any fail:** log each failure as `FB-N: <check #, symptom> (source: deploy-check, priority: hi)` under "Feedback / Review Tasks" — guardrail failures (checks 7–8) are always `hi`. Set "Next expected" to the top failure.
- Do not tag or publish a release until a fully clean run is recorded. Update rules: `.claude/skills/update-status/SKILL.md`.
