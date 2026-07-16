import { Link } from "react-router-dom";

import { DetailField, DetailGrid, DetailSection } from "@/components/detail-ui";
import { EmptyState } from "@/components/empty-state";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { KubeObject } from "@/lib/api";

// Ingress detail (Story 7.2): the routing rules (host → path → backend) as a
// table, plus TLS config. Backends link to their Service; TLS secrets link to
// their Secret. Rendered from the generic object (all fields are present there).

interface IngressBackend {
  service?: { name?: string; port?: { number?: number; name?: string } };
  resource?: { kind?: string; name?: string };
}

interface IngressPath {
  path?: string;
  pathType?: string;
  backend?: IngressBackend;
}

interface IngressRule {
  host?: string;
  http?: { paths?: IngressPath[] };
}

interface IngressTLS {
  hosts?: string[];
  secretName?: string;
}

export function IngressDetail({ object, namespace }: { object: KubeObject; namespace: string }) {
  const spec = (object.spec ?? {}) as {
    ingressClassName?: string;
    defaultBackend?: IngressBackend;
    rules?: IngressRule[];
    tls?: IngressTLS[];
  };
  const status = (object.status ?? {}) as {
    loadBalancer?: { ingress?: { ip?: string; hostname?: string }[] };
  };
  const rules = spec.rules ?? [];
  const tls = spec.tls ?? [];
  const address = (status.loadBalancer?.ingress ?? [])
    .map((i) => i.ip || i.hostname)
    .filter(Boolean)
    .join(", ");

  return (
    <div className="space-y-6">
      <DetailGrid>
        <DetailField label="Class" value={spec.ingressClassName} />
        <DetailField label="Address" value={address} />
        <DetailField label="Default backend">
          <BackendLink backend={spec.defaultBackend} namespace={namespace} />
        </DetailField>
      </DetailGrid>

      <DetailSection title="Rules">
        {rules.length === 0 ? (
          <EmptyState message="This ingress defines no rules." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Host</TableHead>
                <TableHead>Path</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Backend</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rules.flatMap((rule, ri) =>
                (rule.http?.paths ?? [{}]).map((p, pi) => (
                  <TableRow key={`${ri}-${pi}`}>
                    <TableCell>{rule.host || "*"}</TableCell>
                    <TableCell>{p.path || "/"}</TableCell>
                    <TableCell className="text-muted-foreground">{p.pathType || "—"}</TableCell>
                    <TableCell>
                      <BackendLink backend={p.backend} namespace={namespace} />
                    </TableCell>
                  </TableRow>
                )),
              )}
            </TableBody>
          </Table>
        )}
      </DetailSection>

      <DetailSection title="TLS">
        {tls.length === 0 ? (
          <EmptyState message="No TLS configured." />
        ) : (
          <ul className="space-y-2 text-sm">
            {tls.map((t, i) => (
              <li key={t.secretName ?? i} className="rounded-md border p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-muted-foreground">Secret</span>
                  {t.secretName ? (
                    <Link
                      to={`/resources/core/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(t.secretName)}`}
                      className="font-medium underline-offset-4 hover:underline"
                    >
                      {t.secretName}
                    </Link>
                  ) : (
                    <span>—</span>
                  )}
                </div>
                <div className="mt-1 flex flex-wrap gap-2">
                  {(t.hosts ?? []).map((h) => (
                    <Badge key={h} variant="secondary" className="font-normal">
                      {h}
                    </Badge>
                  ))}
                </div>
              </li>
            ))}
          </ul>
        )}
      </DetailSection>
    </div>
  );
}

/** Renders a backend as a link to its Service (or plain text for a resource
 *  backend / no backend). */
function BackendLink({ backend, namespace }: { backend?: IngressBackend; namespace: string }) {
  if (backend?.service?.name) {
    const svc = backend.service;
    const port = svc.port?.number ?? svc.port?.name;
    return (
      <Link
        to={`/resources/core/v1/services/${encodeURIComponent(namespace)}/${encodeURIComponent(svc.name!)}`}
        className="font-medium underline-offset-4 hover:underline"
      >
        {svc.name}
        {port !== undefined ? `:${port}` : ""}
      </Link>
    );
  }
  if (backend?.resource?.name) {
    return (
      <span>
        {backend.resource.kind}/{backend.resource.name}
      </span>
    );
  }
  return <span className="text-muted-foreground">—</span>;
}
