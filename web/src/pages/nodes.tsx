import { AlertCircle, RefreshCw } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
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
import { useNodes } from "@/hooks/use-nodes";
import { ApiError } from "@/lib/api";

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
          <NodesError error={error} />
        ) : data.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No nodes found in this cluster.
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Version</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.map((node) => (
                <TableRow key={node.name}>
                  <TableCell className="font-medium">{node.name}</TableCell>
                  <TableCell>
                    <Badge variant={statusVariant(node.status)}>{node.status}</Badge>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{node.version}</TableCell>
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

function NodesError({ error }: { error: Error }) {
  const detail =
    error instanceof ApiError ? `${error.message} (${error.code})` : error.message;
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>Failed to load nodes</AlertTitle>
      <AlertDescription>{detail}</AlertDescription>
    </Alert>
  );
}
