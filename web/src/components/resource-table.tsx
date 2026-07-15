import { type ColumnDef } from "@tanstack/react-table";
import { useMemo } from "react";
import { Link } from "react-router-dom";

import { WorkloadTable } from "@/components/workload-table";
import { formatAge } from "@/lib/age";
import type { ResourceColumn, ResourceRow } from "@/lib/api";

/** Turns the server column config into TanStack column defs. The name column
 *  links to the object's detail route; the age column sorts by the raw
 *  timestamp (epoch) while displaying a relative age. */
function buildColumnDefs(
  columns: ResourceColumn[],
  detailHref: (row: ResourceRow) => string,
): ColumnDef<ResourceRow>[] {
  return columns.map((col): ColumnDef<ResourceRow> => {
    switch (col.id) {
      case "name":
        return {
          id: "name",
          header: col.header,
          accessorFn: (row) => row.name,
          cell: ({ row }) => (
            <Link
              to={detailHref(row.original)}
              className="font-medium text-foreground underline-offset-4 hover:underline"
            >
              {row.original.name}
            </Link>
          ),
        };
      case "namespace":
        return {
          id: "namespace",
          header: col.header,
          accessorFn: (row) => row.namespace ?? "",
          cell: ({ row }) => (
            <span className="text-muted-foreground">{row.original.namespace ?? "—"}</span>
          ),
        };
      case "age":
        return {
          id: "age",
          header: col.header,
          accessorFn: (row) => (row.creationTimestamp ? Date.parse(row.creationTimestamp) : 0),
          sortingFn: "basic",
          cell: ({ row }) => (
            <span className="text-muted-foreground">{formatAge(row.original.creationTimestamp)}</span>
          ),
        };
      default:
        return {
          id: col.id,
          header: col.header,
          accessorFn: (row) => String((row as unknown as Record<string, unknown>)[col.id] ?? ""),
        };
    }
  });
}

interface ResourceTableProps {
  columns: ResourceColumn[];
  rows: ResourceRow[];
  detailHref: (row: ResourceRow) => string;
}

/** The generic-engine list table. Builds server-described column defs, then
 *  delegates sorting and rendering to the shared WorkloadTable. */
export function ResourceTable({ columns, rows, detailHref }: ResourceTableProps) {
  const columnDefs = useMemo(() => buildColumnDefs(columns, detailHref), [columns, detailHref]);
  return <WorkloadTable columns={columnDefs} rows={rows} />;
}
