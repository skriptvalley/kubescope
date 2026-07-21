# Seed the EKS cluster with the SAME fixtures the kind testenv uses (Story A
# manifests) so Kubescope is exercised against an identical workload set.
#
# Runs at apply time via kubectl against a throwaway kubeconfig minted from the
# cluster — deliberately NOT the terraform kubernetes provider, which would need
# the cluster reachable at plan time and make `terraform validate` require a live
# cluster (ADR-0010). Requires aws + kubectl on the host.
resource "null_resource" "seed" {
  depends_on = [module.eks]

  triggers = {
    cluster  = module.eks.cluster_name
    manifest = filemd5("${path.module}/../testenv/manifests/dev.yaml")
  }

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    command     = <<-EOT
      set -euo pipefail
      command -v aws >/dev/null     || { echo "aws CLI not found — needed to seed the cluster." >&2; exit 1; }
      command -v kubectl >/dev/null || { echo "kubectl not found — needed to seed the cluster." >&2; exit 1; }

      kubeconfig="$(mktemp)"
      trap 'rm -f "$kubeconfig"' EXIT
      aws eks update-kubeconfig \
        --name "${module.eks.cluster_name}" \
        --region "${var.region}" \
        --kubeconfig "$kubeconfig" >/dev/null

      # The cluster-admin access entry can take a few seconds to propagate right
      # after creation — retry the apply briefly before giving up.
      for attempt in 1 2 3 4 5; do
        if kubectl --kubeconfig "$kubeconfig" apply -f "${path.module}/../testenv/manifests/dev.yaml"; then
          exit 0
        fi
        echo "seed apply attempt $attempt failed; retrying in 15s..." >&2
        sleep 15
      done
      echo "seed apply failed after retries" >&2
      exit 1
    EOT
  }
}
