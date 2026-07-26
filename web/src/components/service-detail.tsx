import { Link } from "react-router-dom";

import { DetailField, DetailGrid, DetailSection, LabelBadges } from "@/components/detail-ui";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { ServicePortForwardControls } from "@/components/port-forward-controls";
import { StatusBadge } from "@/components/status-badge";
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
import { useServiceDetail } from "@/hooks/use-service-detail";
import type { EndpointAddressSummary, ServiceDetail as ServiceDetailData } from "@/lib/api";

// Service detail (Story 7.2). The generic object gives the spec; this view adds
// the resolved Endpoints — the ready and not-ready backing pods (the Service's
// matching pod list), each linked to its pod detail via targetRef. FB-13 adds
// the load-balanced port-forward over exactly those ready endpoints.

/** Service ports that a forward can target: port-forwarding is TCP-only. */
function forwardablePorts(data: ServiceDetailData): number[] {
  return data.ports.filter((p) => (p.protocol || "TCP") === "TCP").map((p) => p.port);
}

export function ServiceDetail({ namespace, name }: { namespace: string; name: string }) {
  const { data, isPending, isError, error, refetch } = useServiceDetail(namespace, name);
  const readOnly = useReadOnly();

  if (isPending) return <Skeleton className="h-40 w-full" data-testid="service-detail-loading" />;
  if (isError) {
    return <ErrorState error={error} onRetry={() => refetch()} title="Failed to load service" />;
  }

  const forwardable = forwardablePorts(data);

  return (
    <div className="space-y-6">
      <DetailGrid>
        <DetailField label="Type" value={data.type} />
        <DetailField label="Cluster IP" value={data.clusterIP} />
        <DetailField label="Session Affinity" value={data.sessionAffinity} />
        <DetailField label="External IPs" value={(data.externalIPs ?? []).join(", ")} />
      </DetailGrid>

      <DetailSection title="Ports">
        <ServicePorts data={data} />
      </DetailSection>

      <DetailSection title="Selector">
        <LabelBadges pairs={data.selector} />
      </DetailSection>

      <DetailSection title="Endpoints">
        <Endpoints data={data} />
      </DetailSection>

      {!readOnly && forwardable.length > 0 && (
        <ServicePortForwardControls
          namespace={namespace}
          service={name}
          ports={forwardable}
          readyEndpoints={data.readyAddresses.length}
        />
      )}
    </div>
  );
}

function ServicePorts({ data }: { data: ServiceDetailData }) {
  if (data.ports.length === 0) return <EmptyState message="This service defines no ports." />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Port</TableHead>
          <TableHead>Protocol</TableHead>
          <TableHead>Target</TableHead>
          <TableHead>Node Port</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {data.ports.map((p, i) => (
          <TableRow key={`${p.name ?? p.port}-${i}`}>
            <TableCell>{p.name || "—"}</TableCell>
            <TableCell>{p.port}</TableCell>
            <TableCell>{p.protocol}</TableCell>
            <TableCell>{p.targetPort || "—"}</TableCell>
            <TableCell>{p.nodePort || "—"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function Endpoints({ data }: { data: ServiceDetailData }) {
  const total = data.readyAddresses.length + data.notReadyAddresses.length;
  if (total === 0) {
    return (
      <EmptyState
        message={
          data.endpointsFound
            ? "No backing pods match this service's selector yet."
            : "No endpoints (a headless service or no ready backends)."
        }
      />
    );
  }
  return (
    <div className="space-y-4" data-testid="service-endpoints">
      {data.readyAddresses.length > 0 && (
        <AddressList title="Ready" tone="ok" addresses={data.readyAddresses} />
      )}
      {data.notReadyAddresses.length > 0 && (
        <AddressList title="Not ready" tone="warn" addresses={data.notReadyAddresses} />
      )}
    </div>
  );
}

function AddressList({
  title,
  tone,
  addresses,
}: {
  title: string;
  tone: "ok" | "warn";
  addresses: EndpointAddressSummary[];
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <StatusBadge tone={tone}>{title}</StatusBadge>
        <span className="text-xs text-muted-foreground">{addresses.length}</span>
      </div>
      <ul className="space-y-1">
        {addresses.map((a) => (
          <li key={a.ip} className="flex flex-wrap items-center gap-2 text-sm">
            <code className="font-mono text-xs">{a.ip}</code>
            <PodLink address={a} />
            {a.nodeName && <span className="text-xs text-muted-foreground">on {a.nodeName}</span>}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Links a backing address to its pod detail when it targets a Pod. */
function PodLink({ address }: { address: EndpointAddressSummary }) {
  const ref = address.targetRef;
  if (!ref || ref.kind !== "Pod" || !ref.namespace) {
    return null;
  }
  return (
    <Link
      to={`/resources/core/v1/pods/${encodeURIComponent(ref.namespace)}/${encodeURIComponent(ref.name)}`}
      className="font-medium text-foreground underline-offset-4 hover:underline"
    >
      {ref.name}
    </Link>
  );
}
