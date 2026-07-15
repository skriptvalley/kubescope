import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { StatusTone } from "@/lib/workload-status";

const toneClasses: Record<StatusTone, string> = {
  ok: "border-transparent bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",
  warn: "border-transparent bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
  progress: "border-transparent bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  neutral: "",
};

/** A small status pill tinted by tone. Neutral falls back to the secondary
 *  badge so it stays theme-consistent. */
export function StatusBadge({
  tone,
  children,
  className,
}: {
  tone: StatusTone;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Badge
      variant={tone === "neutral" ? "secondary" : "outline"}
      className={cn("font-medium", toneClasses[tone], className)}
    >
      {children}
    </Badge>
  );
}
