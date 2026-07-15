// Status tone classification for workload views — maps a kubectl-style pod
// STATUS or an event type to a visual tone. Pure so the mapping is unit-tested
// once and reused by the list, detail and events-panel badges.

export type StatusTone = "ok" | "warn" | "progress" | "neutral";

const WARN_STATUSES = new Set([
  "CrashLoopBackOff",
  "Error",
  "Failed",
  "Evicted",
  "OOMKilled",
  "ImagePullBackOff",
  "ErrImagePull",
  "CreateContainerConfigError",
  "InvalidImageName",
  "Unknown",
  "NodeLost",
]);

const OK_STATUSES = new Set(["Running", "Completed", "Succeeded", "Ready"]);

/** Classifies a pod's display status into a tone. Init:*, Pending and other
 *  transitional states read as "progress"; crash/error states as "warn". */
export function podStatusTone(status: string): StatusTone {
  if (OK_STATUSES.has(status)) return "ok";
  if (WARN_STATUSES.has(status)) return "warn";
  if (status.startsWith("Init:") || status.endsWith("Error") || status.endsWith("BackOff")) {
    return status.startsWith("Init:") ? "progress" : "warn";
  }
  if (status === "Terminating") return "warn";
  return "progress";
}

/** Warning events are visually distinct; everything else is neutral. */
export function eventTypeTone(type: string): StatusTone {
  return type === "Warning" ? "warn" : "neutral";
}
