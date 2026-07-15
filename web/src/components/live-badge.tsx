import { cn } from "@/lib/utils";
import type { StreamStatus } from "@/lib/stream";

const config: Record<StreamStatus, { label: string; dot: string; text: string }> = {
  live: {
    label: "Live",
    dot: "bg-emerald-500",
    text: "text-emerald-700 dark:text-emerald-300",
  },
  connecting: {
    label: "Connecting…",
    dot: "bg-amber-500 animate-pulse",
    text: "text-amber-700 dark:text-amber-300",
  },
  stale: {
    label: "Reconnecting…",
    dot: "bg-red-500",
    text: "text-red-700 dark:text-red-300",
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
      <span className={cn("h-2 w-2 rounded-full", dot)} aria-hidden="true" />
      {label}
    </span>
  );
}
