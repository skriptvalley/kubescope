import { cn } from "@/lib/utils";
import type { StreamStatus } from "@/lib/stream";

// Dusk tones: live→brand, connecting→highlight, stale→destructive.
const config: Record<StreamStatus, { label: string; dot: string; text: string }> = {
  live: {
    label: "Live",
    dot: "bg-brand",
    text: "text-badge-brand-fg",
  },
  connecting: {
    label: "Connecting…",
    dot: "bg-highlight animate-pulse",
    text: "text-badge-hl-fg",
  },
  stale: {
    label: "Reconnecting…",
    dot: "bg-destructive",
    text: "text-destructive",
  },
};

/** A compact connection indicator for live SSE-backed views: a colored dot plus
 *  a label. Stale surfaces that the stream dropped and polling has taken over. */
export function LiveBadge({ status, className }: { status: StreamStatus; className?: string }) {
  const { label, dot, text } = config[status];
  return (
    <span
      className={cn("inline-flex items-center gap-1.5 text-xs font-medium", text, className)}
      role="status"
      aria-label={`Live updates: ${label}`}
      title={status === "live" ? "Receiving live updates" : "Live updates unavailable — polling for changes"}
    >
      <span className={cn("h-[7px] w-[7px] rounded-full", dot)} aria-hidden="true" />
      {label}
    </span>
  );
}
