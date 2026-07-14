import { describe, expect, it } from "vitest";

import { formatAge } from "./age";

const now = new Date("2026-07-14T12:00:00Z");

describe("formatAge", () => {
  it("returns an em dash for missing or unparseable input", () => {
    expect(formatAge(undefined, now)).toBe("—");
    expect(formatAge("not-a-date", now)).toBe("—");
  });

  it("formats across seconds, minutes, hours, days and years", () => {
    expect(formatAge("2026-07-14T11:59:30Z", now)).toBe("30s");
    expect(formatAge("2026-07-14T11:30:00Z", now)).toBe("30m");
    expect(formatAge("2026-07-14T06:00:00Z", now)).toBe("6h");
    expect(formatAge("2026-07-10T12:00:00Z", now)).toBe("4d");
    expect(formatAge("2024-07-14T12:00:00Z", now)).toBe("2y");
  });

  it("clamps a future timestamp to 0s", () => {
    expect(formatAge("2026-07-14T12:00:30Z", now)).toBe("0s");
  });
});
