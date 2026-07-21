// The load-bearing status→tone rule (ADR-0009 / Dusk). One classifier maps a
// kubectl-style pod STATUS (or an event type / restart count) to a tone family;
// status-badge.tsx renders each tone as a Dusk tint. Pure, so the mapping is
// unit-tested once and reused by every list, detail, events and condition badge.
//
// Tone families (internal names → Dusk render): ok→brand (teal, healthy),
// progress→highlight (pumpkin, transitional), warn→destructive (red, failing),
// neutral→muted (terminal/unknown). Faithful to the design's tone(): Running→ok,
// Pending/Init:*/Creating→progress, Crash/Failed/*Error/Evicted→warn, terminal
// Completed/Succeeded→neutral. The failing set is a superset of the design's
// literal list (ImagePullBackOff, OOMKilled, …) so real failures never read as
// neutral.

export type StatusTone = "ok" | "warn" | "progress" | "neutral";

/** Healthy, running states → brand. */
const OK_STATUSES = new Set(["Running", "Ready"]);

/** Explicit failing states → destructive. `*Error`/`*BackOff` also match below. */
const WARN_STATUSES = new Set([
  "CrashLoopBackOff",
  "Error",
  "Failed",
  "Evicted",
  "OOMKilled",
  "ImagePullBackOff",
  "ErrImagePull",
  "CreateContainerConfigError",
  "CreateContainerError",
  "InvalidImageName",
  "Unknown",
  "NodeLost",
]);

/** Transitional (pending/creating/terminating) states → highlight. */
const PROGRESS_STATUSES = new Set([
  "Pending",
  "ContainerCreating",
  "PodInitializing",
  "Terminating",
]);

/** Classifies a pod's display status into a tone. Init:* and other transitional
 *  states read as "progress"; crash/error/backoff as "warn"; terminal
 *  Completed/Succeeded (and anything unrecognized) as "neutral". */
export function podStatusTone(status: string): StatusTone {
  if (OK_STATUSES.has(status)) return "ok";
  if (status.startsWith("Init:")) return "progress";
  if (PROGRESS_STATUSES.has(status)) return "progress";
  if (WARN_STATUSES.has(status)) return "warn";
  if (status.endsWith("Error") || status.endsWith("BackOff")) return "warn";
  return "neutral";
}

/** Restart-count tone: 0 → neutral, 1–5 → progress (highlight), >5 → warn
 *  (destructive). Drives the RESTARTS cell color/weight (design threshold). */
export function restartTone(restarts: number): StatusTone {
  if (restarts > 5) return "warn";
  if (restarts > 0) return "progress";
  return "neutral";
}

/** Warning events are visually distinct (destructive); everything else neutral. */
export function eventTypeTone(type: string): StatusTone {
  return type === "Warning" ? "warn" : "neutral";
}
