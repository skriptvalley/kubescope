import { describe, expect, it } from "vitest";

import { ApiError } from "@/lib/api";
import { errorTitle } from "@/lib/error-title";

describe("errorTitle", () => {
  it("maps known ApiError codes to friendly titles", () => {
    expect(errorTitle(new ApiError("gone", "not_found", 404))).toBe("Not found");
    expect(errorTitle(new ApiError("nope", "forbidden", 403))).toBe("Access denied");
    expect(errorTitle(new ApiError("down", "cluster_unreachable", 502))).toBe("Cluster unreachable");
  });

  it("falls back for unmapped codes and plain errors", () => {
    expect(errorTitle(new ApiError("boom", "weird_code", 500), "Failed to load")).toBe(
      "Failed to load",
    );
    expect(errorTitle(new Error("plain"), "Failed to load")).toBe("Failed to load");
    expect(errorTitle(new Error("plain"))).toBe("Something went wrong");
  });
});
