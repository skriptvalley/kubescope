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

// RBAC detail views (Story 7.3): Role/ClusterRole rules as a table, bindings'
// subjects + roleRef (linked to the referenced role), and ServiceAccount secrets
// / image pull secrets. Rendered from the generic object.

interface PolicyRule {
  apiGroups?: string[];
  resources?: string[];
  resourceNames?: string[];
  verbs?: string[];
  nonResourceURLs?: string[];
}

/** Role / ClusterRole: the policy rules flattened into a readable table
 *  (apiGroups / resources / verbs) instead of a raw YAML wall. */
export function RoleDetail({ object }: { object: KubeObject }) {
  const rules = (object.rules ?? []) as PolicyRule[];
  if (rules.length === 0) {
    return (
      <DetailSection title="Rules">
        <EmptyState message="This role grants no rules." />
      </DetailSection>
    );
  }
  return (
    <DetailSection title="Rules">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>API Groups</TableHead>
            <TableHead>Resources</TableHead>
            <TableHead>Resource Names</TableHead>
            <TableHead>Verbs</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rules.map((rule, i) => (
            <TableRow key={i}>
              <TableCell>{joinOrDash(rule.apiGroups, '""')}</TableCell>
              <TableCell>{joinOrDash(rule.resources ?? rule.nonResourceURLs)}</TableCell>
              <TableCell className="text-muted-foreground">{joinOrDash(rule.resourceNames)}</TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-1">
                  {(rule.verbs ?? []).map((v) => (
                    <Badge key={v} variant="secondary" className="font-normal">
                      {v}
                    </Badge>
                  ))}
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </DetailSection>
  );
}

interface Subject {
  kind?: string;
  name?: string;
  namespace?: string;
  apiGroup?: string;
}

interface RoleRef {
  apiGroup?: string;
  kind?: string;
  name?: string;
}

/** RoleBinding / ClusterRoleBinding: the roleRef (linked to the referenced role)
 *  and the subject list. `namespace` is the binding's namespace (undefined for a
 *  ClusterRoleBinding), used to link a namespaced Role reference. */
export function BindingDetail({ object, namespace }: { object: KubeObject; namespace?: string }) {
  const roleRef = (object.roleRef ?? {}) as RoleRef;
  const subjects = (object.subjects ?? []) as Subject[];

  return (
    <div className="space-y-6">
      <DetailSection title="Role reference">
        <p className="text-sm">
          <RoleRefLink roleRef={roleRef} namespace={namespace} />
        </p>
      </DetailSection>

      <DetailSection title="Subjects">
        {subjects.length === 0 ? (
          <EmptyState message="This binding has no subjects." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Kind</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Namespace</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {subjects.map((s, i) => (
                <TableRow key={`${s.kind}-${s.name}-${i}`}>
                  <TableCell>{s.kind || "—"}</TableCell>
                  <TableCell className="font-medium">{s.name || "—"}</TableCell>
                  <TableCell className="text-muted-foreground">{s.namespace || "—"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </DetailSection>
    </div>
  );
}

/** Links a roleRef to the referenced Role/ClusterRole detail. A ClusterRole is
 *  cluster-scoped; a Role lives in the binding's namespace. */
function RoleRefLink({ roleRef, namespace }: { roleRef: RoleRef; namespace?: string }) {
  if (!roleRef.name) return <span className="text-muted-foreground">—</span>;
  let to: string | undefined;
  if (roleRef.kind === "ClusterRole") {
    to = `/resources/rbac.authorization.k8s.io/v1/clusterroles/${encodeURIComponent(roleRef.name)}`;
  } else if (roleRef.kind === "Role" && namespace) {
    to = `/resources/rbac.authorization.k8s.io/v1/roles/${encodeURIComponent(namespace)}/${encodeURIComponent(roleRef.name)}`;
  }
  const label = `${roleRef.kind ?? "Role"}/${roleRef.name}`;
  return to ? (
    <Link to={to} className="font-medium underline-offset-4 hover:underline">
      {label}
    </Link>
  ) : (
    <span className="font-medium">{label}</span>
  );
}

/** ServiceAccount: namespace, mountable secrets and image pull secrets. */
export function ServiceAccountDetail({
  object,
  namespace,
}: {
  object: KubeObject;
  namespace: string;
}) {
  const secrets = (object.secrets ?? []) as { name?: string }[];
  const pullSecrets = (object.imagePullSecrets ?? []) as { name?: string }[];
  const automount = object.automountServiceAccountToken as boolean | undefined;

  return (
    <div className="space-y-6">
      <DetailGrid>
        <DetailField label="Namespace" value={namespace} />
        <DetailField
          label="Automount token"
          value={automount === undefined ? "default" : automount ? "true" : "false"}
        />
      </DetailGrid>

      <DetailSection title="Secrets">
        <SecretRefs namespace={namespace} refs={secrets} empty="No mountable secrets." />
      </DetailSection>

      <DetailSection title="Image pull secrets">
        <SecretRefs namespace={namespace} refs={pullSecrets} empty="No image pull secrets." />
      </DetailSection>
    </div>
  );
}

function SecretRefs({
  namespace,
  refs,
  empty,
}: {
  namespace: string;
  refs: { name?: string }[];
  empty: string;
}) {
  const named = refs.filter((r) => r.name);
  if (named.length === 0) return <EmptyState message={empty} />;
  return (
    <ul className="space-y-1 text-sm">
      {named.map((r) => (
        <li key={r.name}>
          <Link
            to={`/resources/core/v1/secrets/${encodeURIComponent(namespace)}/${encodeURIComponent(r.name!)}`}
            className="font-medium underline-offset-4 hover:underline"
          >
            {r.name}
          </Link>
        </li>
      ))}
    </ul>
  );
}

/** Joins a string list with commas, or a dash when empty. */
function joinOrDash(values: string[] | undefined, emptyLabel = "—"): string {
  if (!values || values.length === 0) return "—";
  return values.map((v) => (v === "" ? emptyLabel : v)).join(", ");
}
