#!/usr/bin/env bash
# Run the Kubescope image against the local kind cluster (ADR-0004).
#
# For a regular kubeconfig with embedded certs and a reachable apiserver the
# plain command is all you need:
#   docker run --rm -p 8080:8080 -v ~/.kube/config:/kubeconfig:ro ghcr.io/skriptvalley/kubescope:latest
#
# Local clusters (kind/minikube/k3d) advertise 127.0.0.1, which inside a
# container is the container itself. This script prepares a container-friendly
# kubeconfig per OS: --network host on Linux, a host.docker.internal rewrite
# (+ insecure-skip-tls-verify, local dev only) on macOS/Windows.
set -euo pipefail

IMAGE="${IMAGE:-ghcr.io/skriptvalley/kubescope:latest}"
KIND_CLUSTER="${KIND_CLUSTER:-kubescope}"
PORT="${PORT:-8080}"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
kubeconfig="$workdir/kubeconfig"

kind get kubeconfig --name "$KIND_CLUSTER" > "$kubeconfig"

# No exec: the EXIT trap must still fire to remove the credentials copy.
case "$(uname -s)" in
  Linux)
    echo ">> Linux: using --network host with the kind kubeconfig as-is"
    # --user: the image runs as nonroot (uid 65532); a bind mount keeps host
    # ownership/mode, so run as the host user to keep the kubeconfig readable.
    docker run --rm --network host \
      --user "$(id -u):$(id -g)" \
      -v "$kubeconfig:/kubeconfig:ro" \
      -e KUBESCOPE_LISTEN_ADDR="0.0.0.0:$PORT" \
      "$IMAGE"
    ;;
  Darwin|MINGW*|MSYS*)
    echo ">> macOS/Windows: rewriting apiserver to host.docker.internal (TLS verify off — local dev only)"
    sed -i.bak \
      -e 's#server: https://127.0.0.1:#server: https://host.docker.internal:#' \
      -e 's#certificate-authority-data:.*#insecure-skip-tls-verify: true#' \
      "$kubeconfig"
    docker run --rm -p "$PORT:8080" \
      -v "$kubeconfig:/kubeconfig:ro" \
      "$IMAGE"
    ;;
  *)
    echo "unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac
