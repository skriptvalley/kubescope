#!/usr/bin/env bash
# Sprint-0 exit criterion, scripted: build the image, run it against kind with
# a mounted kubeconfig, and assert the node list comes back through the full
# stack (container -> kubeconfig -> apiserver -> JSON -> SPA shell).
set -euo pipefail

IMAGE="${IMAGE:-ghcr.io/skriptvalley/kubescope:latest}"
KIND_CLUSTER="${KIND_CLUSTER:-kubescope}"
PORT="${PORT:-18080}"
CONTAINER="kubescope-smoke"

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

for tool in docker kind curl; do
  command -v "$tool" >/dev/null || { echo "FATAL: $tool not found" >&2; exit 1; }
done

echo ">> [1/5] Ensuring kind cluster '$KIND_CLUSTER'"
make kind-up

echo ">> [2/5] Building host-arch image $IMAGE"
make docker-build-local

echo ">> [3/5] Preparing container-friendly kubeconfig"
workdir="$(mktemp -d)"
kubeconfig="$workdir/kubeconfig"
kind get kubeconfig --name "$KIND_CLUSTER" > "$kubeconfig"

run_args=(-d --rm --name "$CONTAINER" -v "$kubeconfig:/kubeconfig:ro")
if [ "$(uname -s)" = "Linux" ]; then
  run_args+=(--network host -e KUBESCOPE_LISTEN_ADDR="0.0.0.0:$PORT")
else
  # macOS/Windows: 127.0.0.1 inside the container is the container itself
  # (ADR-0004). Rewrite to host.docker.internal; the cert lacks that SAN, so
  # TLS verification is disabled — acceptable for a local smoke only.
  sed -i.bak \
    -e 's#server: https://127.0.0.1:#server: https://host.docker.internal:#' \
    -e 's#certificate-authority-data:.*#insecure-skip-tls-verify: true#' \
    "$kubeconfig"
  run_args+=(-p "$PORT:8080")
fi

cleanup() {
  docker stop "$CONTAINER" >/dev/null 2>&1 || true
  rm -rf "$workdir"
}
trap cleanup EXIT

echo ">> [4/5] Starting container"
docker run "${run_args[@]}" "$IMAGE" >/dev/null

base="http://127.0.0.1:$PORT"
echo ">> [5/5] Asserting endpoints on $base"

for i in $(seq 1 30); do
  if curl -fsS "$base/healthz" >/dev/null 2>&1; then break; fi
  [ "$i" = 30 ] && { echo "FATAL: /healthz never came up"; docker logs "$CONTAINER" || true; exit 1; }
  sleep 1
done
echo "   healthz: ok"

nodes_json="$(curl -fsS "$base/api/v1/nodes")" || {
  echo "FATAL: /api/v1/nodes failed"; docker logs "$CONTAINER" || true; exit 1; }
echo "   nodes: $nodes_json"
echo "$nodes_json" | grep -q "\"name\":\"$KIND_CLUSTER-control-plane\"" || {
  echo "FATAL: node list missing $KIND_CLUSTER-control-plane"; exit 1; }
echo "$nodes_json" | grep -q '"status":"Ready"' || {
  echo "FATAL: control-plane node not Ready"; exit 1; }

curl -fsS "$base/" | grep -q "<title>Kubescope</title>" || {
  echo "FATAL: SPA shell not served at /"; exit 1; }
echo "   spa: ok"

echo ""
echo "SMOKE PASSED: node list served end-to-end from the container."
echo "Cluster '$KIND_CLUSTER' left running (make kind-down to remove)."
