import { RefreshCw } from "lucide-react";

import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { NodeActions } from "@/components/node-actions";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useReadOnly } from "@/hooks/use-config";
import { useNodes } from "@/hooks/use-nodes";

function statusVariant(status: string): "default" | "secondary" | "destructive" {
  switch (status) {
    case "Ready":
      return "default";
    case "NotReady":
      return "destructive";
    default:
      return "secondary";
  }
}

export function NodesPage() {
  const { data, isPending, isError, error, refetch, isFetching } = useNodes();
  const readOnly = useReadOnly();

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div className="space-y-1.5">
          <CardTitle>Nodes</CardTitle>
          <CardDescription>Nodes in the current kubeconfig context</CardDescription>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={isFetching}
          aria-label="Refresh nodes"
        >
          <RefreshCw className={isFetching ? "animate-spin" : undefined} />
          Refresh
        </Button>
      </CardHeader>
      <CardContent>
        {isPending ? (
          <NodesSkeleton />
        ) : isError ? (
          <ErrorState error={error} onRetry={() => refetch()} title="Failed to load nodes" />
        ) : data.length === 0 ? (
          <EmptyState message="No nodes found in this cluster." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Schedulable</TableHead>
                <TableHead>Version</TableHead>
                {!readOnly && <TableHead className="text-right">Actions</TableHead>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((node) => (
                <TableRow key={node.name}>
                  <TableCell className="font-medium">{node.name}</TableCell>
                  <TableCell>
                    <Badge variant={statusVariant(node.status)}>{node.status}</Badge>
                  </TableCell>
                  <TableCell>
                    {node.unschedulable ? (
                      <Badge variant="secondary">Cordoned</Badge>
                    ) : (
                      <span className="text-sm text-muted-foreground">Schedulable</span>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{node.version}</TableCell>
                  {!readOnly && (
                    <TableCell>
                      <NodeActions node={node} />
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function NodesSkeleton() {
  return (
    <div className="space-y-2" data-testid="nodes-loading">
      {[0, 1, 2].map((row) => (
        <Skeleton key={row} className="h-9 w-full" />
      ))}
    </div>
  );
}
