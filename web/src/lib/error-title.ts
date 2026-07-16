import { ApiError } from "@/lib/api";

// Maps a structured ApiError code to a friendly heading, shared by every error
// state so the code→title table lives in one place (Sprint 7).

const CODE_TITLES: Record<string, string> = {
  not_found: "Not found",
  forbidden: "Access denied",
  unknown_resource: "Unknown resource",
  unknown_workload: "Unknown workload",
  invalid_scope: "Invalid namespace scope",
  kubeconfig_unavailable: "Kubeconfig unavailable",
  cluster_unreachable: "Cluster unreachable",
  // FB-6 failure taxonomy codes.
  connection_refused: "Connection refused",
  dns: "DNS lookup failed",
  tls_cert: "TLS verification failed",
  exec_plugin_missing: "Credential plugin unavailable",
  auth_expired: "Authentication expired",
  timeout: "Cluster timed out",
  apiserver_5xx: "API server error",
  kubeconfig_invalid: "Invalid kubeconfig",
  kubeconfig_set_disabled: "Runtime kubeconfig disabled",
};

/** Resolves a friendly heading for an error, preferring the ApiError code map
 *  and falling back to the caller's fallback for unmapped codes/plain errors. */
export function errorTitle(error: Error, fallback = "Something went wrong"): string {
  if (error instanceof ApiError) return CODE_TITLES[error.code] ?? fallback;
  return fallback;
}
