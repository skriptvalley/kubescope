import { Link } from "react-router-dom";

import { DetailField, DetailGrid, DetailSection, LabelBadges } from "@/components/detail-ui";
import { EmptyState } from "@/components/empty-state";
import { StatusBadge } from "@/components/status-badge";
import type { KubeObject } from "@/lib/api";
import type { StatusTone } from "@/lib/workload-status";

// Storage detail views (Story 7.4): PVC, PV and StorageClass, wired to each other
// (PVC ⇄ PV) and rendering meaningful status for unbound/pending volumes.

/** Maps a PVC/PV phase to a badge tone. */
function phaseTone(phase: string): StatusTone {
  switch (phase) {
    case "Bound":
    case "Available":
      return "ok";
    case "Pending":
      return "progress";
    case "Lost":
    case "Failed":
      return "warn";
    default:
      return "neutral";
  }
}

function shortAccessMode(m: string): string {
  return (
    { ReadWriteOnce: "RWO", ReadOnlyMany: "ROX", ReadWriteMany: "RWX", ReadWriteOncePod: "RWOP" }[
      m
    ] ?? m
  );
}

/** PersistentVolumeClaim: status, capacity, access modes, class, bound PV. */
export function PersistentVolumeClaimDetail({ object }: { object: KubeObject }) {
  const spec = (object.spec ?? {}) as {
    volumeName?: string;
    storageClassName?: string;
    accessModes?: string[];
    volumeMode?: string;
    resources?: { requests?: { storage?: string } };
  };
  const status = (object.status ?? {}) as { phase?: string; capacity?: { storage?: string } };
  const phase = status.phase || "Unknown";
  const capacity = status.capacity?.storage || spec.resources?.requests?.storage;

  return (
    <div className="space-y-6">
      <DetailGrid>
        <DetailField label="Status">
          <StatusBadge tone={phaseTone(phase)}>{phase}</StatusBadge>
        </DetailField>
        <DetailField label="Capacity" value={capacity} />
        <DetailField
          label="Access Modes"
          value={(spec.accessModes ?? []).map(shortAccessMode).join(", ")}
        />
        <DetailField label="Volume Mode" value={spec.volumeMode} />
        <DetailField label="StorageClass" value={spec.storageClassName} />
        <DetailField label="Volume">
          {spec.volumeName ? (
            <Link
              to={`/resources/core/v1/persistentvolumes/${encodeURIComponent(spec.volumeName)}`}
              className="font-medium underline-offset-4 hover:underline"
            >
              {spec.volumeName}
            </Link>
          ) : (
            <span className="text-muted-foreground">Not bound</span>
          )}
        </DetailField>
      </DetailGrid>
    </div>
  );
}

/** PersistentVolume: reclaim policy, capacity, phase, bound claim (linked). */
export function PersistentVolumeDetail({ object }: { object: KubeObject }) {
  const spec = (object.spec ?? {}) as {
    capacity?: { storage?: string };
    accessModes?: string[];
    persistentVolumeReclaimPolicy?: string;
    storageClassName?: string;
    volumeMode?: string;
    claimRef?: { namespace?: string; name?: string };
  };
  const status = (object.status ?? {}) as { phase?: string };
  const phase = status.phase || "Unknown";
  const claim = spec.claimRef;

  return (
    <div className="space-y-6">
      <DetailGrid>
        <DetailField label="Status">
          <StatusBadge tone={phaseTone(phase)}>{phase}</StatusBadge>
        </DetailField>
        <DetailField label="Capacity" value={spec.capacity?.storage} />
        <DetailField
          label="Access Modes"
          value={(spec.accessModes ?? []).map(shortAccessMode).join(", ")}
        />
        <DetailField label="Reclaim Policy" value={spec.persistentVolumeReclaimPolicy} />
        <DetailField label="StorageClass" value={spec.storageClassName} />
        <DetailField label="Source" value={volumeSource(spec as Record<string, unknown>)} />
        <DetailField label="Claim">
          {claim?.name ? (
            <Link
              to={`/resources/core/v1/persistentvolumeclaims/${encodeURIComponent(claim.namespace ?? "")}/${encodeURIComponent(claim.name)}`}
              className="font-medium underline-offset-4 hover:underline"
            >
              {claim.namespace ? `${claim.namespace}/` : ""}
              {claim.name}
            </Link>
          ) : (
            <span className="text-muted-foreground">Unbound</span>
          )}
        </DetailField>
      </DetailGrid>
    </div>
  );
}

/** StorageClass: provisioner, default marker, policies and parameters. */
export function StorageClassDetail({ object }: { object: KubeObject }) {
  const provisioner = object.provisioner as string | undefined;
  const reclaimPolicy = (object.reclaimPolicy as string | undefined) ?? "Delete";
  const bindingMode = (object.volumeBindingMode as string | undefined) ?? "Immediate";
  const allowExpansion = object.allowVolumeExpansion as boolean | undefined;
  const parameters = (object.parameters ?? {}) as Record<string, string>;
  const anns = object.metadata?.annotations ?? {};
  const isDefault =
    anns["storageclass.kubernetes.io/is-default-class"] === "true" ||
    anns["storageclass.beta.kubernetes.io/is-default-class"] === "true";

  return (
    <div className="space-y-6">
      <DetailGrid>
        <DetailField label="Provisioner" value={provisioner} />
        <DetailField label="Default">
          {isDefault ? <StatusBadge tone="ok">Yes</StatusBadge> : <span>No</span>}
        </DetailField>
        <DetailField label="Reclaim Policy" value={reclaimPolicy} />
        <DetailField label="Binding Mode" value={bindingMode} />
        <DetailField
          label="Allow Expansion"
          value={allowExpansion === undefined ? "false" : allowExpansion ? "true" : "false"}
        />
      </DetailGrid>

      <DetailSection title="Parameters">
        {Object.keys(parameters).length === 0 ? (
          <EmptyState message="No parameters." />
        ) : (
          <LabelBadges pairs={parameters} />
        )}
      </DetailSection>
    </div>
  );
}

// Non-source PV spec keys — everything else that is an object is the volume
// source (hostPath, nfs, csi, …), so we report the first such key's name.
const NON_SOURCE_PV_KEYS = new Set([
  "capacity",
  "accessModes",
  "claimRef",
  "persistentVolumeReclaimPolicy",
  "storageClassName",
  "mountOptions",
  "volumeMode",
  "nodeAffinity",
]);

function volumeSource(spec: Record<string, unknown>): string {
  for (const [k, v] of Object.entries(spec)) {
    if (!NON_SOURCE_PV_KEYS.has(k) && typeof v === "object" && v !== null) {
      return k;
    }
  }
  return "";
}
