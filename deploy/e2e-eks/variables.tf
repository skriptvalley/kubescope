# All AWS specifics are variables with cheap, minimal defaults. Override in a
# terraform.tfvars (gitignored) or with -var flags.

variable "region" {
  description = "AWS region for the ephemeral EKS test cluster (default: Mumbai, INR-billed)."
  type        = string
  default     = "ap-south-1"
}

variable "cluster_name" {
  description = "Name of the ephemeral EKS cluster."
  type        = string
  default     = "kubescope-e2e"
}

variable "cluster_version" {
  description = "Kubernetes control-plane version — must be an EKS-supported version at apply time (override if this default has aged out)."
  type        = string
  default     = "1.33"
}

variable "vpc_cidr" {
  description = "CIDR block for the throwaway test VPC."
  type        = string
  default     = "10.42.0.0/16"
}

variable "cluster_endpoint_public_access_cidrs" {
  description = "CIDRs allowed to reach the public EKS API endpoint. Defaults to the whole internet for convenience (IAM auth is still required); set to [\"<your-ip>/32\"] for a tighter posture."
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "node_instance_type" {
  description = <<-EOT
    EC2 instance type for the managed node group. t3.micro is NOT viable — its
    EKS max-pods is 4 (ENI/IP limit) and it has only 1 GiB RAM, so the
    aws-node/kube-proxy/CoreDNS system pods leave no room for the seed set.
    t3.small/medium is the floor; t3.medium gives comfortable headroom.
  EOT
  type        = string
  default     = "t3.medium"
}

variable "node_desired_size" {
  description = "Desired worker node count (2 gives a realistic multi-node graph: DaemonSet spread, endpoints across nodes)."
  type        = number
  default     = 2
}

variable "node_min_size" {
  description = "Minimum worker node count."
  type        = number
  default     = 1
}

variable "node_max_size" {
  description = "Maximum worker node count."
  type        = number
  default     = 3
}
