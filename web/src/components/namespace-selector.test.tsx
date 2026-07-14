import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ALL_NAMESPACES, NamespaceSelector } from "./namespace-selector";

describe("NamespaceSelector", () => {
  it("offers all-namespaces plus each namespace", () => {
    render(
      <NamespaceSelector value={ALL_NAMESPACES} onChange={() => {}} namespaces={["default", "kube-system"]} />,
    );
    expect(screen.getByRole("option", { name: "All namespaces" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "default" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "kube-system" })).toBeInTheDocument();
  });

  it("reflects the selected namespace", () => {
    render(
      <NamespaceSelector value="kube-system" onChange={() => {}} namespaces={["default", "kube-system"]} />,
    );
    expect((screen.getByLabelText("Namespace") as HTMLSelectElement).value).toBe("kube-system");
  });

  it("fires onChange with the chosen namespace", () => {
    const onChange = vi.fn();
    render(
      <NamespaceSelector value={ALL_NAMESPACES} onChange={onChange} namespaces={["default", "kube-system"]} />,
    );
    fireEvent.change(screen.getByLabelText("Namespace"), { target: { value: "default" } });
    expect(onChange).toHaveBeenCalledWith("default");
  });
});
