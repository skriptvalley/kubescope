import { describe, expect, it } from "vitest";

import { tokenizeYamlLine } from "./yaml-highlight";

describe("tokenizeYamlLine", () => {
  it("splits a mapping line into indent, key, colon and value", () => {
    expect(tokenizeYamlLine("  name: nginx")).toEqual([
      { text: "  ", kind: "plain" },
      { text: "name", kind: "key" },
      { text: ":", kind: "plain" },
      { text: " nginx", kind: "value" },
    ]);
  });

  it("keeps colons inside the value with the value", () => {
    const tokens = tokenizeYamlLine("  image: nginx:1.25");
    expect(tokens.find((t) => t.kind === "key")?.text).toBe("image");
    expect(tokens.find((t) => t.kind === "value")?.text).toBe(" nginx:1.25");
  });

  it("marks a whole comment line", () => {
    expect(tokenizeYamlLine("# a comment")).toEqual([{ text: "# a comment", kind: "comment" }]);
  });

  it("handles a key with no value", () => {
    expect(tokenizeYamlLine("metadata:")).toEqual([
      { text: "metadata", kind: "key" },
      { text: ":", kind: "plain" },
    ]);
  });

  it("treats a non-mapping line as plain", () => {
    expect(tokenizeYamlLine("  - scalar-item")).toEqual([{ text: "  - scalar-item", kind: "plain" }]);
  });
});
