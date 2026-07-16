import { useState } from "react";

import { DetailSection } from "@/components/detail-ui";
import { EmptyState } from "@/components/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { KubeObject } from "@/lib/api";

// ConfigMap detail (Story 7.1): the data keys with their values. Long values are
// collapsed behind a toggle so a big config blob doesn't dominate the view.
// binaryData keys are listed (as binary) but their bytes are not rendered.

const COLLAPSE_THRESHOLD = 400;

/** A single ConfigMap data entry: key + value, collapsible when long. */
function ConfigMapRow({ keyName, value }: { keyName: string; value: string }) {
  const long = value.length > COLLAPSE_THRESHOLD || value.split("\n").length > 8;
  const [expanded, setExpanded] = useState(false);
  const shown = long && !expanded ? value.slice(0, COLLAPSE_THRESHOLD) : value;

  return (
    <div className="space-y-1 rounded-md border p-3">
      <div className="flex items-center justify-between gap-2">
        <code className="truncate font-mono text-xs font-medium" title={keyName}>
          {keyName}
        </code>
        {long && (
          <Button variant="ghost" size="sm" onClick={() => setExpanded((e) => !e)}>
            {expanded ? "Collapse" : "Expand"}
          </Button>
        )}
      </div>
      <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded bg-muted/60 px-2 py-1 font-mono text-xs">
        {shown}
        {long && !expanded ? "…" : ""}
      </pre>
    </div>
  );
}

export function ConfigMapDetail({ object }: { object: KubeObject }) {
  const data = (object.data ?? {}) as Record<string, string>;
  const binaryData = (object.binaryData ?? {}) as Record<string, string>;
  const keys = Object.keys(data).sort();
  const binaryKeys = Object.keys(binaryData).sort();

  if (keys.length === 0 && binaryKeys.length === 0) {
    return (
      <DetailSection title="Data">
        <EmptyState message="This ConfigMap has no data." />
      </DetailSection>
    );
  }

  return (
    <DetailSection title="Data">
      <div className="space-y-2">
        {keys.map((k) => (
          <ConfigMapRow key={k} keyName={k} value={data[k]} />
        ))}
        {binaryKeys.map((k) => (
          <div key={k} className="flex items-center gap-2 rounded-md border p-3">
            <code className="truncate font-mono text-xs font-medium" title={k}>
              {k}
            </code>
            <Badge variant="outline" className="font-normal">
              binary
            </Badge>
          </div>
        ))}
      </div>
    </DetailSection>
  );
}
