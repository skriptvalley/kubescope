// A compact client-side pod display-status, for the detail header badge. The
// authoritative kubectl-style status is computed server-side (PodSummary); this
// is a lightweight subset over a raw Pod object covering the common states
// (waiting/terminated reasons, Terminating, phase). Pure and unit-tested.

interface ContainerState {
  waiting?: { reason?: string };
  terminated?: { reason?: string; exitCode?: number };
}
interface ContainerStatus {
  state?: ContainerState;
  ready?: boolean;
}

export interface PodStatusObject {
  metadata?: { deletionTimestamp?: string };
  status?: {
    phase?: string;
    reason?: string;
    containerStatuses?: ContainerStatus[];
    initContainerStatuses?: ContainerStatus[];
  };
}

const TRANSIENT_WAITING = new Set(["PodInitializing", "ContainerCreating"]);

/** Derives a kubectl-ish STATUS from a raw Pod object. */
export function podDisplayStatus(object: PodStatusObject): string {
  const s = object.status ?? {};
  const terminal = s.phase === "Succeeded" || s.phase === "Failed";
  if (object.metadata?.deletionTimestamp && !terminal) return "Terminating";
  if (s.reason) return s.reason; // Evicted, NodeLost, …

  const all = [...(s.initContainerStatuses ?? []), ...(s.containerStatuses ?? [])];
  for (const cs of all) {
    const w = cs.state?.waiting;
    if (w?.reason && !TRANSIENT_WAITING.has(w.reason)) return w.reason; // CrashLoopBackOff, ImagePullBackOff, …
    const t = cs.state?.terminated;
    if (t?.reason && t.reason !== "Completed") return t.reason;
  }
  if ((s.containerStatuses ?? []).some((cs) => cs.state?.waiting?.reason === "ContainerCreating")) {
    return "ContainerCreating";
  }
  return s.phase ?? "Unknown";
}
