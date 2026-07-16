import type { ReactNode } from "react";

import { Badge } from "@/components/ui/badge";

// Shared detail-view primitives (Sprint 7). The typed detail components
// (ConfigMap/Service/Ingress/RBAC/Storage) render through these so the config,
// networking, RBAC and storage views share one look — a key/value grid, labeled
// sections, and label/selector badge rows.

/** One labeled value in a `dl` grid. Pass either a string `value` or richer
 *  `children` (e.g. a cross-link). Renders a dash when empty. */
export function DetailField({
  label,
  value,
  children,
}: {
  label: string;
  value?: ReactNode;
  children?: ReactNode;
}) {
  const content = children ?? value;
  const empty = content === undefined || content === null || content === "";
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-all text-sm font-medium">{empty ? "—" : content}</dd>
    </div>
  );
}

/** A titled section with an optional right-aligned action, matching the generic
 *  Summary view's panels. */
export function DetailSection({
  title,
  action,
  children,
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

/** A responsive `dl` grid for DetailField children. */
export function DetailGrid({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">{children}</dl>;
}

/** Renders a label/selector map as `k=v` badges, or a "None" note when empty. */
export function LabelBadges({ pairs }: { pairs: Record<string, string> | undefined }) {
  const entries = Object.entries(pairs ?? {}).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0) return <p className="text-sm text-muted-foreground">None</p>;
  return (
    <div className="flex flex-wrap gap-2">
      {entries.map(([k, v]) => (
        <Badge key={k} variant="secondary" className="font-normal">
          {k}={v}
        </Badge>
      ))}
    </div>
  );
}
