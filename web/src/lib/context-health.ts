import type { ContextHealth } from "@/lib/api";

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
