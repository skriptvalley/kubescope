# Sprint 8 — Hardening & release

## Context recap
Read before starting (in order):
1. `STATUS.md` — current state + any feedback tasks.
2. `docs/ARCHITECTURE.md` — component you are touching.
3. ADRs: `docs/adr/0005-security-posture-read-only-and-secret-masking.md` (auth toggle, exposure warnings), `docs/adr/0002-single-binary-embedded-spa.md` (the release artifact), `docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md` (gotchas the docs pass must cover).
One sprint per session. Do not pull work forward. Rules in `CLAUDE.md` apply.

## Sprint goal
Ship v0.1.0: optional auth, a security pass, CI with multi-arch image publishing, tagged release, and complete docs.

## Stories

### Story 8.1 — Optional auth: basic-auth toggle (OIDC if time permits) via `KUBESCOPE_AUTH_MODE`
Auth middleware keyed on `KUBESCOPE_AUTH_MODE` (`none` | `basic` | `oidc`, default `none`). `basic` is required this sprint; `oidc` is stretch only.
**Acceptance criteria:**
- [ ] `KUBESCOPE_AUTH_MODE=basic` gates all API and SPA routes with HTTP basic auth; missing/wrong credentials get 401.
- [ ] `KUBESCOPE_AUTH_MODE=none` (default) preserves current behavior; `/healthz` stays unauthenticated in all modes.
- [ ] Credential source for basic mode is decided and documented; any new config is recorded by updating ADR-0005 and the env var docs — do not add config silently.
- [ ] Credentials are never logged; auth failures are logged without the attempted values.
- [ ] Stretch: `oidc` mode behind the same toggle; if skipped, it fails fast at startup with a clear "not implemented" error.

### Story 8.2 — Security pass: finalize read-only enforcement, secret-handling audit
Sweep the whole API surface against ADR-0005 before release.
**Acceptance criteria:**
- [ ] Every mutating endpoint (apply, scale, rollout-restart, delete, cordon/drain, exec, port-forward start, secret reveal write paths) is verified blocked under `KUBESCOPE_READ_ONLY=true`; regression tests cover the full route table.
- [ ] Secret-handling audit: no code path logs or returns unmasked Secret data outside the explicit reveal flow.
- [ ] Default bind remains `127.0.0.1:8080` for the bare binary; Docker image binds `0.0.0.0:8080` via env as designed.
- [ ] README and docs carry the warning: do not expose Kubescope publicly without auth + network controls.

### Story 8.3 — CI + multi-arch image publish (lint/test/build on PR; image on tag)
GitHub Actions: PR pipeline plus tag-triggered image publishing.
**Acceptance criteria:**
- [ ] On PR: lint (Go + frontend), Go and frontend unit tests, and full binary build (embedded SPA) must pass; failures block merge.
- [ ] On tag `v*`: multi-arch (amd64 + arm64) image built and pushed to `ghcr.io/skriptvalley/kubescope` with the version tag and `latest`.
- [ ] Pipeline runs green on this sprint's PR before merge.
- [ ] Published image manifest lists both architectures.

### Story 8.4 — v0.1.0: tag, GitHub release, docs pass, optional Helm chart
Cut the release and make the docs true. Docs pass must cover the ADR-0004 gotchas end to end.
**Acceptance criteria:**
- [ ] Docs pass: README quickstart verified against the published image; ADR-0004 gotchas documented for users — exec-plugin auth (EKS/GKE), file-path certs needing extra mounts, local kind/minikube requiring `--network host` (Linux) or `host.docker.internal` rewrite (Mac/Win).
- [ ] Env var reference complete and accurate, including `KUBESCOPE_AUTH_MODE`.
- [ ] `v0.1.0` tag pushed; GitHub release created with changelog summarizing sprints 0–8.
- [ ] `docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest` works against the released image on a kind cluster.
- [ ] Stretch: minimal Helm chart under `deploy/`; if skipped, note it in `STATUS.md` as a follow-up, not a blocker.

## Task checklist
- [ ] Implement auth middleware in `internal/server` switched by `KUBESCOPE_AUTH_MODE`; wire into config loading/validation.
- [ ] Decide + document basic-auth credential source; update ADR-0005 and env docs accordingly.
- [ ] Add auth-mode unit tests: none passes through, basic 401/200 matrix, `/healthz` exemption, unknown mode fails startup.
- [ ] Write read-only regression suite iterating every mutating route.
- [ ] Run the secret-handling audit (grep logs/handlers for Secret data paths); fix findings.
- [ ] Add GitHub Actions PR workflow: lint + test + build (Go and frontend, embedded-SPA binary).
- [ ] Add tag workflow: buildx multi-arch (amd64+arm64) image publish to `ghcr.io/skriptvalley/kubescope`.
- [ ] Docs pass: README, `docs/PRD.md` success criteria check, env var table, ADR-0004 gotchas section, public-exposure warning.
- [ ] Write the v0.1.0 changelog from `STATUS.md` sprint history.
- [ ] Run `.claude/skills/deploy-check` pre-release verification against kind.
- [ ] Tag `v0.1.0`, verify the tag workflow publishes the image, create the GitHub release.
- [ ] Smoke the published image with the canonical Docker one-liner on a kind cluster.
- [ ] Stretch: scaffold minimal Helm chart under `deploy/` and document `helm install`.

## Testing requirements
- Unit: auth middleware matrix (mode × credentials × route incl. `/healthz`); config validation for `KUBESCOPE_AUTH_MODE`; read-only regression across the full mutating route table.
- CI: PR pipeline (lint/test/build) green is itself an exit criterion; tag pipeline verified by inspecting the published multi-arch manifest.
- Manual kind smoke: released image via the canonical Docker run one-liner — context switch, resource browse, one mutation in a scratch namespace with auth enabled, read-only mode blocking writes.

## Definition of Done
- Compiles/builds; lint clean.
- Unit tests for new logic pass.
- Manual smoke against kind for cluster-touching features.
- Docs updated if behavior/API changed.

## End-of-session actions
1. Run `make test` and `make lint`.
2. Update `STATUS.md` (last work + type, next expected, checkboxes).
3. Commit (Conventional Commits), push branch `sprint-8/<slug>`, open PR.
4. Print a concise summary: outcome + blockers only.
