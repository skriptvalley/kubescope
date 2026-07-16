import { describe, expect, it } from "vitest";

import { ApiError } from "@/lib/api";
import { errorTitle } from "@/lib/error-title";

describe("errorTitle", () => {
  it("maps known ApiError codes to friendly titles", () => {
    expect(errorTitle(new ApiError("gone", "not_found", 404))).toBe("Not found");
    expect(errorTitle(new ApiError("nope", "forbidden", 403))).toBe("Access denied");
    expect(errorTitle(new ApiError("down", "cluster_unreachable", 502))).toBe("Cluster unreachable");
  });

  it("maps the FB-6 failure-taxonomy codes", () => {
    expect(errorTitle(new ApiError("x", "connection_refused", 502))).toBe("Connection refused");
    expect(errorTitle(new ApiError("x", "dns", 502))).toBe("DNS lookup failed");
    expect(errorTitle(new ApiError("x", "tls_cert", 502))).toBe("TLS verification failed");
    expect(errorTitle(new ApiError("x", "exec_plugin_missing", 502))).toBe(
      "Credential plugin unavailable",
    );
    expect(errorTitle(new ApiError("x", "auth_expired", 401))).toBe("Authentication expired");
    expect(errorTitle(new ApiError("x", "timeout", 504))).toBe("Cluster timed out");
    expect(errorTitle(new ApiError("x", "apiserver_5xx", 502))).toBe("API server error");
    expect(errorTitle(new ApiError("x", "kubeconfig_invalid", 422))).toBe("Invalid kubeconfig");
    expect(errorTitle(new ApiError("x", "kubeconfig_set_disabled", 403))).toBe(
      "Runtime kubeconfig disabled",
    );
  });

  it("falls back for unmapped codes and plain errors", () => {
    expect(errorTitle(new ApiError("boom", "weird_code", 500), "Failed to load")).toBe(
      "Failed to load",
    );
    expect(errorTitle(new Error("plain"), "Failed to load")).toBe("Failed to load");
    expect(errorTitle(new Error("plain"))).toBe("Something went wrong");
  });
});
