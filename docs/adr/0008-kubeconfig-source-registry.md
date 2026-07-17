# 0008. Kubeconfig source registry: files + directories, kubectl merge

- **Status:** Accepted (supersedes [0007](0007-runtime-kubeconfig-source.md))
- **Date:** 2026-07-17

## Context

Users keep kubeconfigs as many files (one per cluster/team), not one merged file, and want to add a cluster to a *running* Kubescope from the UI (FB-8). [0007](0007-runtime-kubeconfig-source.md) landed a single runtime-settable path; a single path cannot express "this directory of files" and forces users to merge files by hand.

The Docker analysis that bounds every design here (recorded so it is never re-litigated):

- Path controls resolve on the **process's** filesystem. Bare binary: any host path. Docker: only paths under mounts made at container creation — **runtime mounting does not exist**, and every workaround (Docker socket access, a privileged host agent) is root-equivalent on the host: rejected under [0005](0005-security-posture-read-only-and-secret-masking.md).
- Kubescope can never copy a host file it cannot read, and never writes into the read-only kubeconfig mount ([0004](0004-cluster-auth-and-kubeconfig-in-docker.md)).
- Therefore the only safe runtime-add in Docker is a **directory mounted once** (`-v ~/.kube:/kubeconfigs:ro`): bind-mounted directories reflect files added on the host live.

## Decision

`kube.Manager` reads an ordered **registry of kubeconfig sources**; each source is a file **or a directory**. This supersedes 0007's single path — the registry keeps 0007's gating, validation and in-memory semantics, generalized:

- **Env baseline:** `KUBESCOPE_KUBECONFIG` accepts a list separated by the OS path-list separator (`:` on Unix), each entry a file or directory. No new env var. A single path behaves exactly as before. `$KUBECONFIG` fallback splits the same way (matching kubectl), and the default container mount point `/kubeconfig` is detected whether it is a file **or a directory** — `-v ~/.kube:/kubeconfig:ro` works with no env var at all.
- **Merge = kubectl semantics:** the resolved file list feeds `clientcmd` loading-rules **Precedence** — first occurrence of a context/cluster/user name wins; `current-context` comes from the first file that sets one. Shadowed names are computed per source and surfaced in the listing, never silently swallowed.
- **Directory expansion:** non-recursive, lexicographic; hidden files, subdirectories, files >1 MiB, and unparseable files are skipped with a **per-file classified status** — never a global failure. Expansion happens at load time on every request (no background watcher, scan failures are not cached), so a file dropped into a mounted directory appears without restart; a UI "Rescan" is just a refetch.
- **API:** `GET /api/v1/kubeconfigs` (sources in precedence order: path, kind `file|dir`, status `ok|missing|unparseable|empty`, per-file expansion for directories, contexts contributed, shadowed names) is always readable, like `/setup`. `POST /api/v1/kubeconfigs {"path"}` appends; `DELETE /api/v1/kubeconfigs {"path"}` removes. Both mutations replace 0007's `PUT /api/v1/kubeconfig` (pre-v0.1.0, no compatibility burden) and keep its exact gating: enabled only when `KUBESCOPE_ALLOW_KUBECONFIG_SET=true` (else `403 kubeconfig_set_disabled`), registered inside the read-only-guarded group, auth applies as everywhere.
- **Validate before apply:** a runtime-added path must be absolute and visible; a file must parse and define ≥1 context; a directory must exist and be readable — an **empty** directory is accepted (status `empty`) because "mount a dir, drop files in later" is the point. Any failure returns a classified error (an invisible path names the mounted-directory workflow) and leaves the registry untouched.
- **In-memory overlay:** runtime adds/removes (including removing an env-baseline source) mutate only memory; a restart reverts to the env baseline — 0007's semantics, unchanged.
- **One invalidation path:** every successful registry mutation bumps `SourceGeneration` and fires the source observer (SSE close, exec/port-forward teardown), exactly as 0007's swap did. Passive directory-scan changes (a file appearing/disappearing) deliberately do **not** bump the generation — closing every live stream because an unrelated file landed would punish the common case; the same-name-moved-endpoint edge is covered by the existing probe-driven stale-client eviction and informer rebuild (FB-6). The active-context override is kept across mutations; if its context vanished, `resolveActive` falls back (remaining sources' `current-context`, else the `no_active_context` starter).
- **Secrets:** unchanged from 0007 — paths and parse positions only in errors/logs, contents never.

### Paste/upload re-decision

Accepting pasted or uploaded kubeconfig **content** remains **rejected** for this release. The registry weakens the original motivation (never-mounted files) only slightly — the mounted-directory workflow covers the Docker case with one upfront `-v` — while the objections stand unchanged: request bodies and browser surfaces become a credential path, memory-only content is a second restart-lost source of truth, and any persistence would write secrets to disk. Revisit only if a concrete workflow emerges that a mounted directory cannot serve.

## Consequences

**Positive:**
- Multi-file kubeconfig users (one file per cluster) work without hand-merging; `kubectl`-identical precedence means no new mental model.
- "Mount `~/.kube` once, drop files in" gives Docker users a true runtime-add with zero new attack surface.
- Per-source/per-file statuses make broken files a visible, local problem instead of a global outage.

**Negative:**
- Per-request directory scans + per-file parses cost more than one file parse; acceptable at kubeconfig scale (small files, few sources), and required for watcher-free live re-scan.
- Merge-level shadowing (a context in two files) can surprise; mitigated by surfacing shadowed names in the listing, not solved.
- A passively-changed winner (lexicographically-earlier file dropped in defining an existing name) updates REST clients only on the next successful probe eviction; live informers rebuild via the FB-6 prober. Brief staleness accepted over stream-churn-on-every-file-drop.

## Alternatives considered

- **Keep the single `PUT /api/v1/kubeconfig` and require pre-merged files** — rejected: pushes the merge burden onto users; kubectl already defines list semantics everyone knows.
- **Background fsnotify watcher on directories** — rejected: a watcher adds lifecycle/failure modes and a dependency for something a per-request scan already gives; inotify is unreliable across bind mounts on macOS/Windows Docker anyway.
- **Bump `SourceGeneration` on any resolved file-set change** — rejected: tears down every SSE stream/exec session whenever any file lands in a watched dir; the probe/prober machinery already heals the rare same-name endpoint move.
- **Pasted/uploaded kubeconfig content** — re-examined and still rejected (see above).
- **Docker-socket or privileged-agent runtime mounts** — rejected: root-equivalent on the host ([0005](0005-security-posture-read-only-and-secret-masking.md)).
