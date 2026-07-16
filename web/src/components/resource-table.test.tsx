import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

import type { ResourceColumn, ResourceRow } from "@/lib/api";

import { ResourceTable } from "./resource-table";

const columns: ResourceColumn[] = [
  { id: "name", header: "Name" },
  { id: "namespace", header: "Namespace" },
  { id: "age", header: "Age" },
];

// Deliberately unsorted by both name and timestamp so a monotonic result after
// sorting proves the sort ran — independent of the default sort direction.
const rows: ResourceRow[] = [
  { name: "gamma", namespace: "default", creationTimestamp: "2026-07-12T10:00:00Z" },
  { name: "alpha", namespace: "default", creationTimestamp: "2026-07-14T10:00:00Z" },
  { name: "beta", namespace: "default", creationTimestamp: "2026-07-10T10:00:00Z" },
];

function renderTable() {
  return render(
    <MemoryRouter>
      <ResourceTable columns={columns} rows={rows} detailHref={(r) => `/detail/${r.name}`} />
    </MemoryRouter>,
  );
}

function dataRowNames(): string[] {
  return screen
    .getAllByRole("row")
    .slice(1) // drop the header row
    .map((row) => within(row).getByRole("link").textContent ?? "");
}

describe("ResourceTable", () => {
  it("links each name to its detail route", () => {
    renderTable();
    expect(screen.getByRole("link", { name: "alpha" })).toHaveAttribute("href", "/detail/alpha");
  });

  it("sorts by name when the Name header is clicked", () => {
    renderTable();
    fireEvent.click(screen.getByRole("button", { name: "Sort by Name" }));
    // Alphabetical order (asc or desc) puts "beta" in the middle either way.
    expect(dataRowNames()[1]).toBe("beta");
  });

  it("sorts by timestamp when the Age header is clicked", () => {
    renderTable();
    fireEvent.click(screen.getByRole("button", { name: "Sort by Age" }));
    // Chronological order puts the middle timestamp (gamma, 07-12) in the middle.
    expect(dataRowNames()[1]).toBe("gamma");
  });

  it("renders per-kind enrichment columns from row.cells", () => {
    const enrichedColumns: ResourceColumn[] = [
      { id: "name", header: "Name" },
      { id: "type", header: "Type" },
      { id: "ports", header: "Ports" },
      { id: "age", header: "Age" },
    ];
    const enrichedRows: ResourceRow[] = [
      { name: "web", creationTimestamp: "2026-07-12T10:00:00Z", cells: { type: "ClusterIP", ports: "80/TCP" } },
      { name: "api", creationTimestamp: "2026-07-12T10:00:00Z", cells: { type: "NodePort", ports: "" } }, // empty → dash
    ];
    render(
      <MemoryRouter>
        <ResourceTable
          columns={enrichedColumns}
          rows={enrichedRows}
          detailHref={(r) => `/detail/${r.name}`}
        />
      </MemoryRouter>,
    );
    expect(screen.getByText("ClusterIP")).toBeInTheDocument();
    expect(screen.getByText("80/TCP")).toBeInTheDocument();
    expect(screen.getByText("NodePort")).toBeInTheDocument();
    // An empty cell renders a dash, not a blank column.
    const apiRow = screen.getByRole("link", { name: "api" }).closest("tr")!;
    expect(within(apiRow).getByText("—")).toBeInTheDocument();
  });
});
