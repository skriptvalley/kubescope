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
import { Link, useNavigate } from "react-router-dom";

import { StatusBadge } from "@/components/status-badge";
import { restartClass } from "@/lib/tone-style";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { PodMetrics, PodSummary } from "@/lib/api";
import { formatAge } from "@/lib/age";
import { cn } from "@/lib/utils";
import { podStatusTone } from "@/lib/workload-status";

type Align = "left" | "right";

interface ColMeta {
  align?: Align;
}

export interface PodsTableProps {
  pods: PodSummary[];
  /** `namespace/name` → usage; absent entries render "—". */
  metrics?: Map<string, PodMetrics>;
  showNamespace?: boolean;
  showNode?: boolean;
  showCpuMem?: boolean;
  emptyMessage?: string;
}

function podRoute(p: PodSummary): string {
  return `/resources/core/v1/pods/${encodeURIComponent(p.namespace)}/${encodeURIComponent(p.name)}`;
}

function namespaceRoute(ns: string): string {
  return `/resources/core/v1/namespaces/${encodeURIComponent(ns)}`;
}

/** The Dusk pods table (TanStack), shared by the overview and namespace detail.
 *  Rows link to pod detail; the status/restart/ready colors come from the
 *  centralized tone rule. CPU/Memory columns render "—" without metrics. */
export function PodsTable({
  pods,
  metrics,
  showNamespace = false,
  showNode = false,
  showCpuMem = false,
  emptyMessage = "No pods.",
}: PodsTableProps) {
  const navigate = useNavigate();
  const [sorting, setSorting] = useState<SortingState>([]);

  const columns = useMemo<ColumnDef<PodSummary>[]>(() => {
    const cols: ColumnDef<PodSummary>[] = [
      {
        id: "name",
        header: "Name",
        accessorKey: "name",
        cell: ({ row }) => (
          <span className="font-mono text-xs font-medium text-foreground">{row.original.name}</span>
        ),
      },
    ];
    if (showNamespace) {
      cols.push({
        id: "namespace",
        header: "Namespace",
        accessorKey: "namespace",
        cell: ({ row }) => (
          <Link
            to={namespaceRoute(row.original.namespace)}
            onClick={(e) => e.stopPropagation()}
            className="text-[12.5px] text-muted-foreground no-underline hover:text-primary hover:underline"
          >
            {row.original.namespace}
          </Link>
        ),
      });
    }
    cols.push(
      {
        id: "ready",
        header: "Ready",
        accessorFn: (p) => p.readyContainers,
        cell: ({ row }) => {
          const p = row.original;
          const ok = p.totalContainers > 0 && p.readyContainers === p.totalContainers;
          return (
            <span className={cn("font-mono text-xs", ok ? "text-foreground" : "text-muted-foreground")}>
              {p.ready}
            </span>
          );
        },
      },
      {
        id: "status",
        header: "Status",
        accessorKey: "status",
        cell: ({ row }) => (
          <StatusBadge tone={podStatusTone(row.original.status)} dot>
            {row.original.status}
          </StatusBadge>
        ),
      },
      {
        id: "restarts",
        header: "Restarts",
        accessorKey: "restarts",
        meta: { align: "right" } satisfies ColMeta,
        cell: ({ row }) => (
          <span className={cn("font-mono text-xs", restartClass(row.original.restarts))}>
            {row.original.restarts}
          </span>
        ),
      },
    );
    if (showCpuMem) {
      cols.push(
        {
          id: "cpu",
          header: "CPU",
          enableSorting: false,
          meta: { align: "right" } satisfies ColMeta,
          cell: ({ row }) => (
            <span className="font-mono text-xs text-muted-foreground">
              {metrics?.get(`${row.original.namespace}/${row.original.name}`)?.cpu ?? "—"}
            </span>
          ),
        },
        {
          id: "memory",
          header: "Memory",
          enableSorting: false,
          meta: { align: "right" } satisfies ColMeta,
          cell: ({ row }) => (
            <span className="font-mono text-xs text-muted-foreground">
              {metrics?.get(`${row.original.namespace}/${row.original.name}`)?.memory ?? "—"}
            </span>
          ),
        },
      );
    }
    if (showNode) {
      cols.push({
        id: "node",
        header: "Node",
        accessorFn: (p) => p.node ?? "",
        cell: ({ row }) => (
          <span className="text-[12.5px] text-muted-foreground">{row.original.node || "—"}</span>
        ),
      });
    }
    cols.push({
      id: "age",
      header: "Age",
      accessorFn: (p) => p.creationTimestamp ?? "",
      meta: { align: "right" } satisfies ColMeta,
      cell: ({ row }) => (
        <span className="font-mono text-xs text-muted-foreground">
          {formatAge(row.original.creationTimestamp)}
        </span>
      ),
    });
    return cols;
  }, [metrics, showCpuMem, showNamespace, showNode]);

  const table = useReactTable({
    data: pods,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  const colCount = table.getAllLeafColumns().length;

  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id} className="hover:bg-transparent">
              {group.headers.map((header) => {
                const align = (header.column.columnDef.meta as ColMeta | undefined)?.align ?? "left";
                return (
                  <TableHead key={header.id} className={cn(align === "right" && "text-right")}>
                    {header.column.getCanSort() ? (
                      <button
                        type="button"
                        className={cn(
                          "inline-flex items-center gap-1 hover:text-foreground",
                          align === "right" && "flex-row-reverse",
                        )}
                        onClick={header.column.getToggleSortingHandler()}
                        aria-label={`Sort by ${String(header.column.columnDef.header)}`}
                      >
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        <SortIcon direction={header.column.getIsSorted()} />
                      </button>
                    ) : (
                      flexRender(header.column.columnDef.header, header.getContext())
                    )}
                  </TableHead>
                );
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {table.getRowModel().rows.length === 0 ? (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={colCount} className="py-8 text-center text-sm text-muted-foreground">
                {emptyMessage}
              </TableCell>
            </TableRow>
          ) : (
            table.getRowModel().rows.map((row) => (
              <TableRow
                key={row.id}
                onClick={() => navigate(podRoute(row.original))}
                className="cursor-pointer"
              >
                {row.getVisibleCells().map((cell) => {
                  const align = (cell.column.columnDef.meta as ColMeta | undefined)?.align ?? "left";
                  return (
                    <TableCell key={cell.id} className={cn(align === "right" && "text-right")}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  );
                })}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}

function SortIcon({ direction }: { direction: false | "asc" | "desc" }) {
  const Icon = direction === "asc" ? ArrowUp : direction === "desc" ? ArrowDown : ChevronsUpDown;
  return <Icon className={cn("h-3 w-3", !direction && "opacity-40")} />;
}
