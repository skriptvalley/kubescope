#!/usr/bin/env bash
#
# Kubescope local test environment.
#
# Brings up two kind clusters ("dev" + "prod") seeded with sample resources so
# you can manually exercise Kubescope — context switching, live-updating lists,
# pod log streaming and the events feed — and tears them down again. Everything
# uses an isolated kubeconfig, so your real ~/.kube/config is never touched.
#
# Usage:
#   deploy/testenv/testenv.sh up               # create clusters + apply resources
#   deploy/testenv/testenv.sh down             # delete both clusters
#   deploy/testenv/testenv.sh status           # show clusters + workloads
#   deploy/testenv/testenv.sh run              # build + run kubescope (native) against it
#   deploy/testenv/testenv.sh run --docker     # ...or run the container image instead
#   deploy/testenv/testenv.sh compose-config   # prep kubeconfig for the docker-compose flow
#   deploy/testenv/testenv.sh check            # verify required tools are present
#   deploy/testenv/testenv.sh check --install  # ...and install missing ones (brew)
#
# Env overrides:
#   KUBESCOPE_TESTENV_KUBECONFIG   kubeconfig path (default: <this dir>/kubeconfig)
#   KUBESCOPE_TESTENV_DEV_CLUSTER  dev cluster name  (default: kubescope-dev)
#   KUBESCOPE_TESTENV_PROD_CLUSTER prod cluster name (default: kubescope-prod)
#   KUBESCOPE_IMAGE                image for 'run --docker' (default: ghcr.io/skriptvalley/kubescope:latest)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MANIFESTS="$SCRIPT_DIR/manifests"

KUBECONFIG_FILE="${KUBESCOPE_TESTENV_KUBECONFIG:-$SCRIPT_DIR/kubeconfig}"
DEV_CLUSTER="${KUBESCOPE_TESTENV_DEV_CLUSTER:-kubescope-dev}"
PROD_CLUSTER="${KUBESCOPE_TESTENV_PROD_CLUSTER:-kubescope-prod}"
DEV_CTX="kind-$DEV_CLUSTER"
PROD_CTX="kind-$PROD_CLUSTER"

# Keep every kubectl/kind operation on the isolated kubeconfig.
export KUBECONFIG="$KUBECONFIG_FILE"

# --- logging -----------------------------------------------------------------
if [ -t 1 ]; then
  C_BLUE="\033[34m"; C_GREEN="\033[32m"; C_YELLOW="\033[33m"; C_RED="\033[31m"; C_RESET="\033[0m"
else
  C_BLUE=""; C_GREEN=""; C_YELLOW=""; C_RED=""; C_RESET=""
fi
log()  { printf "${C_BLUE}==>${C_RESET} %s\n" "$*"; }
ok()   { printf "${C_GREEN}✓${C_RESET} %s\n" "$*"; }
warn() { printf "${C_YELLOW}!${C_RESET} %s\n" "$*" >&2; }
die()  { printf "${C_RED}✗ %s${C_RESET}\n" "$*" >&2; exit 1; }

# --- tool checks -------------------------------------------------------------
have() { command -v "$1" >/dev/null 2>&1; }

# require_tools [--install] — fail unless docker (running), kind and kubectl are
# present. With --install, missing kind/kubectl are installed via Homebrew when
# available; Docker must always be installed manually.
require_tools() {
  local install=0
  [ "${1:-}" = "--install" ] && install=1

  if ! have docker; then
    die "docker not found. Install Docker Desktop: https://www.docker.com/products/docker-desktop/"
  fi
  if ! docker info >/dev/null 2>&1; then
    die "the Docker daemon is not running — start Docker Desktop and retry."
  fi
  ok "docker present and running"

  local tool brew_pkg
  for tool in kind kubectl; do
    if have "$tool"; then
      ok "$tool present"
      continue
    fi
    if [ "$install" -eq 1 ] && have brew; then
      case "$tool" in
        kind) brew_pkg="kind" ;;
        kubectl) brew_pkg="kubernetes-cli" ;;
      esac
      log "installing $tool (brew install $brew_pkg) ..."
      brew install "$brew_pkg"
      ok "$tool installed"
    else
      case "$tool" in
        kind) die "kind not found. Install with: brew install kind  (or see https://kind.sigs.k8s.io/docs/user/quick-start/#installation). Re-run with 'check --install' to auto-install." ;;
        kubectl) die "kubectl not found. Install with: brew install kubernetes-cli  (or see https://kubernetes.io/docs/tasks/tools/). Re-run with 'check --install' to auto-install." ;;
      esac
    fi
  done
}

# --- cluster lifecycle -------------------------------------------------------
cluster_exists() { kind get clusters 2>/dev/null | grep -qx "$1"; }

ensure_cluster() {
  local name="$1"
  if cluster_exists "$name"; then
    ok "cluster '$name' already exists"
  else
    log "creating cluster '$name' (this pulls the node image on first run) ..."
    kind create cluster --name "$name" --wait 120s
  fi
  # (Re)write this cluster's context into the isolated kubeconfig, so re-runs
  # work even if the file was removed by a prior 'down'.
  kind export kubeconfig --name "$name" --kubeconfig "$KUBECONFIG_FILE" >/dev/null
}

up() {
  require_tools "${1:-}"
  ensure_cluster "$DEV_CLUSTER"
  ensure_cluster "$PROD_CLUSTER"

  log "applying dev resources → $DEV_CTX"
  kubectl --context "$DEV_CTX" apply -f "$MANIFESTS/dev.yaml"
  log "applying prod resources → $PROD_CTX"
  kubectl --context "$PROD_CTX" apply -f "$MANIFESTS/prod.yaml"

  kubectl config use-context "$DEV_CTX" >/dev/null

  log "waiting for the dev frontend to roll out (best effort) ..."
  kubectl --context "$DEV_CTX" -n web rollout status deploy/frontend --timeout=120s \
    || warn "frontend not ready yet — it will appear live in the UI as pods come up"

  print_summary
}

down() {
  local c
  for c in "$DEV_CLUSTER" "$PROD_CLUSTER"; do
    if cluster_exists "$c"; then
      log "deleting cluster '$c'"
      kind delete cluster --name "$c"
    else
      ok "cluster '$c' already gone"
    fi
  done
  rm -f "$KUBECONFIG_FILE"
  ok "test environment removed"
}

status() {
  log "kind clusters:"
  kind get clusters 2>/dev/null | sed 's/^/  /' || true
  local ctx name
  for ctx in "$DEV_CTX" "$PROD_CTX"; do
    name="${ctx#kind-}"
    if cluster_exists "$name"; then
      printf "\n${C_BLUE}==> workloads in %s${C_RESET}\n" "$ctx"
      kubectl --context "$ctx" get pods -A 2>/dev/null \
        | grep -vE 'kube-system|local-path-storage|kube-node-lease|kube-public' || true
    fi
  done
}

run() {
  local do_build=0 do_docker=0 arg
  for arg in "$@"; do
    case "$arg" in
      --build)  do_build=1 ;;
      --docker) do_docker=1 ;;
      *) die "unknown flag '$arg' for run (try: --build | --docker)" ;;
    esac
  done

  if [ "$do_docker" -eq 1 ]; then
    run_docker "$do_build"
    return
  fi

  # Bare-binary path: build (or rebuild on --build) and exec the native binary.
  have go || die "go not found — needed to build kubescope."
  if [ ! -x "$REPO_ROOT/bin/kubescope" ] || [ "$do_build" -eq 1 ]; then
    log "building kubescope ..."
    make -C "$REPO_ROOT" build
  fi
  cluster_exists "$DEV_CLUSTER" || die "no test environment — run '$0 up' first."
  log "starting kubescope on http://127.0.0.1:8080 (Ctrl-C to stop) ..."
  KUBESCOPE_KUBECONFIG="$KUBECONFIG_FILE" exec "$REPO_ROOT/bin/kubescope"
}

# run_docker <do_build> — run the container image against the test environment.
# Mirrors deploy/run-local.sh's per-OS handling: --network host on Linux, a
# host.docker.internal rewrite (+ insecure-skip-tls-verify, local dev only) on
# macOS/Windows. Credentials only ever live in a trap-cleaned temp copy, so we
# must NOT exec — the EXIT trap has to fire after the container stops.
run_docker() {
  local do_build="$1"
  have docker || die "docker not found — needed for '--docker'. Install Docker Desktop: https://www.docker.com/products/docker-desktop/"
  docker info >/dev/null 2>&1 || die "the Docker daemon is not running — start Docker Desktop and retry."

  local image="${KUBESCOPE_IMAGE:-ghcr.io/skriptvalley/kubescope:latest}"
  if [ "$do_build" -eq 1 ]; then
    log "building image $image (make docker-build) ..."
    make -C "$REPO_ROOT" docker-build
  elif ! docker image inspect "$image" >/dev/null 2>&1; then
    die "image '$image' not found locally — run 'make docker-build' or pass --build."
  fi

  cluster_exists "$DEV_CLUSTER" || die "no test environment — run '$0 up' first."

  # Copy the isolated kubeconfig into a throwaway dir; the EXIT trap removes it.
  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT
  cp "$KUBECONFIG_FILE" "$tmpdir/kubeconfig"

  log "starting kubescope in Docker on http://127.0.0.1:8080 (Ctrl-C to stop) ..."
  case "$(uname -s)" in
    Linux)
      # Local kind apiservers advertise 127.0.0.1; --network host lets the
      # container reach them with the kubeconfig unchanged. --user keeps the
      # bind-mounted copy readable by the nonroot image user. Under host
      # networking, container loopback IS host loopback — bind 127.0.0.1 so an
      # unauthenticated, mutation-capable dashboard is never LAN-exposed
      # (ADR-0005; the image's 0.0.0.0 default assumes an isolated netns).
      docker run --rm --network host \
        --user "$(id -u):$(id -g)" \
        -v "$tmpdir/kubeconfig:/kubeconfig:ro" \
        -e KUBESCOPE_LISTEN_ADDR="127.0.0.1:8080" \
        "$image"
      ;;
    Darwin|MINGW*|MSYS*)
      # 127.0.0.1 inside the container is the container itself; rewrite every
      # cluster entry to host.docker.internal (both dev + prod contexts) and
      # drop TLS verification (local dev only).
      echo ">> macOS/Windows: rewriting apiserver to host.docker.internal (TLS verify off — local dev only)"
      sed -i.bak \
        -e 's#server: https://127.0.0.1:#server: https://host.docker.internal:#' \
        -e 's#certificate-authority-data:.*#insecure-skip-tls-verify: true#' \
        "$tmpdir/kubeconfig"
      # Publish on the host's loopback only — no auth is configured (ADR-0005).
      docker run --rm -p 127.0.0.1:8080:8080 \
        -v "$tmpdir/kubeconfig:/kubeconfig:ro" \
        "$image"
      ;;
    *)
      die "unsupported OS: $(uname -s)"
      ;;
  esac
}

# compose_config — write a container-ready kubeconfig to the fixed path that
# build/docker-compose.yml mounts (build/.e2e-kubeconfig), for the kind compose
# flow. On macOS/Windows the kind apiserver (127.0.0.1 → the container itself)
# is rewritten to host.docker.internal (+ skip TLS verify, local dev only) so a
# bridged container can reach it. On Linux a bridged compose can't reach a
# 127.0.0.1 apiserver — use 'run --docker' (host networking) for kind there.
compose_config() {
  cluster_exists "$DEV_CLUSTER" || die "no test environment — run '$0 up' first."
  local dst="$REPO_ROOT/build/.e2e-kubeconfig"
  cp "$KUBECONFIG_FILE" "$dst"
  case "$(uname -s)" in
    Darwin|MINGW*|MSYS*)
      log "adapting kubeconfig for Docker (host.docker.internal, TLS verify off — local dev only)"
      sed -i.bak \
        -e 's#server: https://127.0.0.1:#server: https://host.docker.internal:#' \
        -e 's#certificate-authority-data:.*#insecure-skip-tls-verify: true#' \
        "$dst"
      rm -f "$dst.bak"
      ;;
    Linux)
      warn "Linux: a bridged docker-compose can't reach a 127.0.0.1 kind apiserver."
      warn "For kind on Linux use 'make testenv-run-docker' (host networking); compose targets kind on macOS/Windows + EKS on any OS."
      ;;
  esac
  chmod 600 "$dst"
  ok "wrote $dst"
  cat <<EOF

Next: launch Kubescope with docker-compose:
  make compose-up      # http://127.0.0.1:8080  (build the image first if needed: make docker-build-local)
  make compose-down    # stop + remove the adapted kubeconfig copy
EOF
}

print_summary() {
  cat <<EOF

$(ok "test environment ready")

  Kubeconfig : $KUBECONFIG_FILE
  Contexts   : $DEV_CTX (active), $PROD_CTX

  Run Kubescope against it:
    $SCRIPT_DIR/testenv.sh run
  or manually:
    make build && KUBESCOPE_KUBECONFIG="$KUBECONFIG_FILE" ./bin/kubescope
  then open http://localhost:8080

  Inspect with kubectl:
    export KUBECONFIG="$KUBECONFIG_FILE"
    kubectl --context $DEV_CTX get pods -A

  Tear down when done:
    $SCRIPT_DIR/testenv.sh down
EOF
}

usage() {
  cat <<EOF
Kubescope local test environment — two kind clusters + sample resources.

Usage: testenv.sh <command>

  up              create the clusters and apply the sample resources
  down            delete both clusters (removes the isolated kubeconfig)
  status          show the clusters and their workloads
  run [--build] [--docker]
                  run kubescope against the environment. Native by default;
                  --docker runs the container image instead (KUBESCOPE_IMAGE,
                  default ghcr.io/skriptvalley/kubescope:latest); --build first
                  rebuilds the binary (or, with --docker, the image).
  compose-config  write a container-ready kubeconfig to build/.e2e-kubeconfig for
                  the docker-compose flow (make compose-up); kind on macOS/Windows
  check [--install]   verify required tools are present (--install: fix via brew)

Everything uses an isolated kubeconfig ($KUBECONFIG_FILE);
your ~/.kube/config is never touched. See deploy/testenv/README.md for details.
EOF
}

case "${1:-}" in
  up)     shift; up "${1:-}" ;;
  down)   down ;;
  status) status ;;
  run)    shift; run "$@" ;;
  compose-config) compose_config ;;
  check|preflight) shift; require_tools "${1:-}"; ok "all required tools present" ;;
  -h|--help|help|"") usage ;;
  *) die "unknown command '$1' (try: up | down | status | run | compose-config | check)" ;;
esac
