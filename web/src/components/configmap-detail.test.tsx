import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ConfigMapDetail } from "@/components/configmap-detail";
import type { KubeObject } from "@/lib/api";

function cm(object: Partial<KubeObject>): KubeObject {
  return { kind: "ConfigMap", ...object } as KubeObject;
}

describe("ConfigMapDetail", () => {
  it("lists data keys with their values", () => {
    render(<ConfigMapDetail object={cm({ data: { "app.conf": "debug=true", port: "8080" } })} />);
    expect(screen.getByText("app.conf")).toBeInTheDocument();
    expect(screen.getByText("debug=true")).toBeInTheDocument();
    expect(screen.getByText("port")).toBeInTheDocument();
  });

  it("collapses a long value behind an Expand toggle", () => {
    const big = "x".repeat(600);
    render(<ConfigMapDetail object={cm({ data: { big } })} />);
    const toggle = screen.getByRole("button", { name: "Expand" });
    // Collapsed: truncated with an ellipsis marker.
    expect(screen.getByText(/x{400}…/)).toBeInTheDocument();
    fireEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Collapse" })).toBeInTheDocument();
  });

  it("marks binary data keys without rendering their bytes", () => {
    render(<ConfigMapDetail object={cm({ binaryData: { "cert.der": "AAAA" } })} />);
    expect(screen.getByText("cert.der")).toBeInTheDocument();
    expect(screen.getByText("binary")).toBeInTheDocument();
    expect(screen.queryByText("AAAA")).toBeNull();
  });

  it("renders an empty state when there is no data", () => {
    render(<ConfigMapDetail object={cm({})} />);
    expect(screen.getByTestId("empty-state")).toHaveTextContent("no data");
  });
});
