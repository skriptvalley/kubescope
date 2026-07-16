import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { BindingDetail, RoleDetail, ServiceAccountDetail } from "@/components/rbac-detail";
import type { KubeObject } from "@/lib/api";

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe("RoleDetail", () => {
  it("renders policy rules as a table", () => {
    const object = {
      rules: [
        { apiGroups: [""], resources: ["pods"], verbs: ["get", "list"] },
        { apiGroups: ["apps"], resources: ["deployments"], verbs: ["*"] },
      ],
    } as KubeObject;
    renderWithRouter(<RoleDetail object={object} />);
    expect(screen.getByText("pods")).toBeInTheDocument();
    expect(screen.getByText("get")).toBeInTheDocument();
    expect(screen.getByText("deployments")).toBeInTheDocument();
    // The empty core apiGroup renders as a quoted placeholder, not blank.
    expect(screen.getByText('""')).toBeInTheDocument();
  });

  it("shows an empty state for a role with no rules", () => {
    renderWithRouter(<RoleDetail object={{ rules: [] } as KubeObject} />);
    expect(screen.getByTestId("empty-state")).toHaveTextContent("no rules");
  });
});

describe("BindingDetail", () => {
  it("links the roleRef to a namespaced Role and lists subjects", () => {
    const object = {
      roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: "reader" },
      subjects: [{ kind: "ServiceAccount", name: "app", namespace: "default" }],
    } as KubeObject;
    renderWithRouter(<BindingDetail object={object} namespace="default" />);
    const link = screen.getByRole("link", { name: "Role/reader" });
    expect(link).toHaveAttribute(
      "href",
      "/resources/rbac.authorization.k8s.io/v1/roles/default/reader",
    );
    expect(screen.getByText("app")).toBeInTheDocument();
  });

  it("links a ClusterRole reference to the cluster-scoped route", () => {
    const object = {
      roleRef: { kind: "ClusterRole", name: "view" },
      subjects: [],
    } as KubeObject;
    renderWithRouter(<BindingDetail object={object} namespace={undefined} />);
    expect(screen.getByRole("link", { name: "ClusterRole/view" })).toHaveAttribute(
      "href",
      "/resources/rbac.authorization.k8s.io/v1/clusterroles/view",
    );
  });
});

describe("ServiceAccountDetail", () => {
  it("links mountable secrets to their Secret detail", () => {
    const object = { secrets: [{ name: "app-token" }] } as KubeObject;
    renderWithRouter(<ServiceAccountDetail object={object} namespace="default" />);
    const link = screen.getByRole("link", { name: "app-token" });
    expect(link).toHaveAttribute("href", "/resources/core/v1/secrets/default/app-token");
  });

  it("shows empty states with no secrets", () => {
    renderWithRouter(<ServiceAccountDetail object={{} as KubeObject} namespace="default" />);
    const empties = screen.getAllByTestId("empty-state");
    expect(within(empties[0]).getByText(/no mountable secrets/i)).toBeInTheDocument();
  });
});
