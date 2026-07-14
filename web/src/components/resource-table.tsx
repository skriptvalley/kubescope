import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatAge } from "@/lib/age";
import type { ResourceColumn, ResourceRow } from "@/lib/api";
import { cn } from "@/lib/utils";

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

export function ResourceTable({ columns, rows, detailHref }: ResourceTableProps) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const columnDefs = useMemo(() => buildColumnDefs(columns, detailHref), [columns, detailHref]);

  const table = useReactTable({
    data: rows,
    columns: columnDefs,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  return (
    <Table>
      <TableHeader>
        {table.getHeaderGroups().map((group) => (
          <TableRow key={group.id}>
            {group.headers.map((header) => {
              const sorted = header.column.getIsSorted();
              return (
                <TableHead key={header.id}>
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 hover:text-foreground"
                    onClick={header.column.getToggleSortingHandler()}
                    aria-label={`Sort by ${String(header.column.columnDef.header)}`}
                  >
                    {flexRender(header.column.columnDef.header, header.getContext())}
                    <SortIcon direction={sorted} />
                  </button>
                </TableHead>
              );
            })}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody>
        {table.getRowModel().rows.map((row) => (
          <TableRow key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <TableCell key={cell.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function SortIcon({ direction }: { direction: false | "asc" | "desc" }) {
  const Icon = direction === "asc" ? ArrowUp : direction === "desc" ? ArrowDown : ChevronsUpDown;
  return <Icon className={cn("h-3.5 w-3.5", !direction && "opacity-40")} />;
}
