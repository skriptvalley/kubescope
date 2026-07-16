import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { IngressDetail } from "@/components/ingress-detail";
import type { KubeObject } from "@/lib/api";

function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe("IngressDetail", () => {
  it("renders rules with backends linking to their Service", () => {
    const object = {
      spec: {
        ingressClassName: "nginx",
        rules: [
          {
            host: "example.com",
            http: {
              paths: [
                {
                  path: "/api",
                  pathType: "Prefix",
                  backend: { service: { name: "api", port: { number: 8080 } } },
                },
              ],
            },
          },
        ],
      },
    } as KubeObject;
    renderWithRouter(<IngressDetail object={object} namespace="default" />);
    expect(screen.getByText("example.com")).toBeInTheDocument();
    expect(screen.getByText("/api")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "api:8080" })).toHaveAttribute(
      "href",
      "/resources/core/v1/services/default/api",
    );
  });

  it("renders TLS config linking to the referenced Secret", () => {
    const object = {
      spec: { tls: [{ hosts: ["example.com"], secretName: "example-tls" }] },
    } as KubeObject;
    renderWithRouter(<IngressDetail object={object} namespace="default" />);
    expect(screen.getByRole("link", { name: "example-tls" })).toHaveAttribute(
      "href",
      "/resources/core/v1/secrets/default/example-tls",
    );
  });

  it("shows empty states with no rules or TLS", () => {
    renderWithRouter(<IngressDetail object={{ spec: {} } as KubeObject} namespace="default" />);
    expect(screen.getByText(/no rules/i)).toBeInTheDocument();
    expect(screen.getByText(/no tls/i)).toBeInTheDocument();
  });
});
