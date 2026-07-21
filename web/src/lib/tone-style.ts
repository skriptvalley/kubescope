import { cn } from "@/lib/utils";
import { restartTone, type StatusTone } from "@/lib/workload-status";

/** The single tone→Dusk-tint mapping (ADR-0009). `pill` tints the badge, `dot`
 *  colors a leading status dot, `text` colors bare text (restart cells, dl
 *  values). One source of truth for every badge, dot, ready/restart color and
 *  condition chip. */
export const toneStyles: Record<StatusTone, { pill: string; dot: string; text: string }> = {
  ok: { pill: "bg-brand/15 text-badge-brand-fg", dot: "bg-brand", text: "text-badge-brand-fg" },
  progress: { pill: "bg-highlight/15 text-badge-hl-fg", dot: "bg-highlight", text: "text-badge-hl-fg" },
  warn: { pill: "bg-destructive/10 text-destructive", dot: "bg-destructive", text: "text-destructive" },
  neutral: { pill: "bg-muted text-muted-foreground", dot: "bg-muted-foreground", text: "text-muted-foreground" },
};

/** Text-color class for a restart count (0 muted / 1–5 highlight / >5 red+bold),
 *  used by the pods tables' RESTARTS cell (design threshold). */
export function restartClass(restarts: number): string {
  const tone = restartTone(restarts);
  return cn(toneStyles[tone].text, tone === "warn" && "font-semibold");
}
