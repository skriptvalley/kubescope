import type { ContextHealth } from "@/lib/api";
import type { StatusTone } from "@/lib/workload-status";

export type BadgeState = {
  label: string;
  variant: "default" | "secondary" | "destructive";
  title: string;
};

/** healthBadge maps a per-context probe result to a status badge. */
export function healthBadge(
  health: ContextHealth | undefined,
  pending: boolean,
): BadgeState {
  if (pending || !health) {
    return { label: "Checking", variant: "secondary", title: "Probing cluster…" };
  }
  if (health.reachable && health.authOK) {
    return {
      label: "Healthy",
      variant: "default",
      title: health.serverVersion ? `Server ${health.serverVersion}` : "Reachable",
    };
  }
  if (health.reachable && !health.authOK) {
    return {
      label: "Auth error",
      variant: "destructive",
      title: health.guidance || health.error || "Authentication failed",
    };
  }
  return {
    label: "Unreachable",
    variant: "destructive",
    title: health.guidance || health.error || "Cluster unreachable",
  };
}

/** Maps a per-context probe to a Dusk tone for the switcher pill and the active-
 *  context status dot: Healthy→ok (teal), Unreachable/Auth error→warn (red),
 *  Checking→neutral. Reuses the shared badge copy/title so the label stays in
 *  one place. */
export function healthTone(
  health: ContextHealth | undefined,
  pending: boolean,
): { tone: StatusTone; label: string; title: string } {
  const b = healthBadge(health, pending);
  const tone: StatusTone =
    b.variant === "default" ? "ok" : b.variant === "destructive" ? "warn" : "neutral";
  return { tone, label: b.label, title: b.title };
}
