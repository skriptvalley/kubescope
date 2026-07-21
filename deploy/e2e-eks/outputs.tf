output "cluster_name" {
  description = "EKS cluster name (consumed by kubeconfig.sh)."
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "EKS API server endpoint (consumed by kubeconfig.sh)."
  value       = module.eks.cluster_endpoint
}

output "cluster_certificate_authority_data" {
  description = "Base64 CA bundle for the API server (embedded into the token-kubeconfig)."
  value       = module.eks.cluster_certificate_authority_data
}

output "region" {
  description = "AWS region the cluster runs in."
  value       = var.region
}

output "teardown_reminder" {
  description = "Loud reminder that this cluster is billed until destroyed."
  value       = "!! BILLED EKS cluster (control plane + ${var.node_desired_size}x ${var.node_instance_type} + NAT gateway). Mint a kubeconfig with 'make e2e-eks-kubeconfig', and run 'make e2e-eks-down' when finished to STOP CHARGES."
}
