import { toneStyles } from "@/lib/tone-style";
import { cn } from "@/lib/utils";
import type { StatusTone } from "@/lib/workload-status";

/** A small Dusk status pill tinted by tone, with an optional leading dot. The
 *  tone→tint mapping lives in lib/tone-style (one source of truth). */
export function StatusBadge({
  tone,
  dot = false,
  children,
  className,
}: {
  tone: StatusTone;
  /** Render a leading status dot (pod/container status pills use it). */
  dot?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  const s = toneStyles[tone];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 whitespace-nowrap rounded-sm px-2 py-0.5 text-xs font-medium",
        s.pill,
        className,
      )}
    >
      {dot && <span className={cn("h-[5px] w-[5px] shrink-0 rounded-full", s.dot)} aria-hidden="true" />}
      {children}
    </span>
  );
}
