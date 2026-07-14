import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { YamlView } from "./yaml-view";

describe("YamlView", () => {
  const yaml = "apiVersion: v1\nkind: Pod\nmetadata:\n  name: nginx";

  it("renders the full YAML text read-only", () => {
    render(<YamlView yaml={yaml} />);
    const view = screen.getByTestId("yaml-view");
    expect(view.textContent).toContain("apiVersion");
    expect(view.textContent).toContain("kind: Pod");
    expect(view.textContent).toContain("name: nginx");
  });

  it("highlights mapping keys", () => {
    render(<YamlView yaml={yaml} />);
    const keySpan = screen.getByText("kind");
    expect(keySpan.className).toMatch(/sky/);
  });
});
