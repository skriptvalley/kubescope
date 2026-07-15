import { Ban, CircleCheck, Play } from "lucide-react";
import { useState } from "react";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { useDrainNode, useSetNodeSchedulable } from "@/hooks/use-mutations";
import { type DrainResult, type NodeSummary } from "@/lib/api";

// Node actions (Story 5.3): cordon/uncordon behind a confirmation, and drain
// behind a typed-name gate that reports per-pod eviction results — a PDB-blocked
// or failed eviction is shown, not swallowed. Rendered only when writable.

export function NodeActions({ node }: { node: NodeSummary }) {
  const [cordonOpen, setCordonOpen] = useState(false);
  const [drainOpen, setDrainOpen] = useState(false);
  const [result, setResult] = useState<DrainResult | null>(null);

  const schedulable = useSetNodeSchedulable();
  const drain = useDrainNode();

  const cordon = !node.unschedulable; // cordon if currently schedulable

  return (
    <div className="flex items-center justify-end gap-2">
      <Button
        variant="outline"
        size="sm"
        onClick={() => setCordonOpen(true)}
        aria-label={cordon ? `Cordon ${node.name}` : `Uncordon ${node.name}`}
      >
        {cordon ? <Ban /> : <CircleCheck />}
        {cordon ? "Cordon" : "Uncordon"}
      </Button>
      <Button variant="outline" size="sm" onClick={() => setDrainOpen(true)} aria-label={`Drain ${node.name}`}>
        <Play /> Drain
      </Button>

      <ConfirmDialog
        open={cordonOpen}
        title={`${cordon ? "Cordon" : "Uncordon"} ${node.name}?`}
        description={
          cordon
            ? "Marks the node unschedulable; running pods stay put, but nothing new schedules here."
            : "Marks the node schedulable again."
        }
        confirmLabel={cordon ? "Cordon" : "Uncordon"}
        pending={schedulable.isPending}
        error={schedulable.error}
        onConfirm={() =>
          schedulable.mutate(
            { name: node.name, cordon },
            { onSuccess: () => setCordonOpen(false) },
          )
        }
        onCancel={() => setCordonOpen(false)}
      />

      <ConfirmDialog
        open={drainOpen}
        destructive
        title={`Drain ${node.name}?`}
        description="Cordons the node, then evicts eligible pods (skipping DaemonSet and static pods). PodDisruptionBudgets are respected — a blocked pod is reported, not force-killed."
        confirmText={node.name}
        confirmTextLabel="Type the node name to confirm"
        confirmLabel="Drain"
        pending={drain.isPending}
        error={drain.error}
        onConfirm={() =>
          drain.mutate(node.name, {
            onSuccess: (res) => {
              setDrainOpen(false);
              setResult(res);
            },
          })
        }
        onCancel={() => setDrainOpen(false)}
      />

      {result && <DrainResultDialog result={result} onClose={() => setResult(null)} />}
    </div>
  );
}

const resultTone: Record<DrainResult["pods"][number]["result"], string> = {
  evicted: "text-emerald-600 dark:text-emerald-400",
  skipped: "text-muted-foreground",
  blocked: "text-amber-600 dark:text-amber-400",
  error: "text-destructive",
};

/** Read-only modal summarizing a drain: counts plus every pod's outcome. */
function DrainResultDialog({ result, onClose }: { result: DrainResult; onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`Drain results for ${result.node}`}
        className="flex max-h-[80vh] w-full max-w-lg flex-col rounded-lg border bg-background p-6 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold">Drain results — {result.node}</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {result.evicted} evicted · {result.skipped} skipped · {result.blocked} blocked ·{" "}
          {result.failed} failed
        </p>

        <div className="mt-4 min-h-0 flex-1 space-y-1 overflow-y-auto">
          {result.pods.length === 0 ? (
            <p className="text-sm text-muted-foreground">No pods were on this node.</p>
          ) : (
            result.pods.map((p) => (
              <div key={`${p.namespace}/${p.name}`} className="rounded border p-2 text-sm">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate font-mono text-xs">
                    {p.namespace}/{p.name}
                  </span>
                  <span className={`shrink-0 text-xs font-medium ${resultTone[p.result]}`}>{p.result}</span>
                </div>
                {p.reason && <p className="mt-0.5 text-xs text-muted-foreground">{p.reason}</p>}
              </div>
            ))
          )}
        </div>

        <div className="mt-4 flex justify-end">
          <Button variant="outline" size="sm" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
    </div>
  );
}
