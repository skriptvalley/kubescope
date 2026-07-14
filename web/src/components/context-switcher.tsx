import { Check, ChevronsUpDown } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  useContexts,
  useContextsHealth,
  useSwitchContext,
} from "@/hooks/use-contexts";
import { healthBadge } from "@/lib/context-health";
import { cn } from "@/lib/utils";

export function ContextSwitcher() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const { data: contexts, isPending, isError } = useContexts();
  const { data: health, isPending: healthPending } = useContextsHealth();
  const switchContext = useSwitchContext();

  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  if (isError) {
    return <span className="text-sm text-destructive">kubeconfig error</span>;
  }

  const active = contexts?.find((c) => c.active);
  const healthByName = new Map((health ?? []).map((h) => [h.name, h]));

  const label = switchContext.isPending
    ? "Switching…"
    : (active?.name ?? (isPending ? "Loading…" : "Select context"));

  return (
    <div className="relative" ref={ref}>
      <Button
        variant="outline"
        size="sm"
        className="min-w-52 justify-between"
        onClick={() => setOpen((o) => !o)}
        disabled={isPending || switchContext.isPending}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label="Switch context"
      >
        <span className="truncate">{label}</span>
        <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
      </Button>
      {open && contexts && contexts.length > 0 && (
        <ul
          role="listbox"
          className="absolute right-0 z-50 mt-1 max-h-80 w-72 overflow-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
        >
          {contexts.map((c) => {
            const badge = healthBadge(healthByName.get(c.name), healthPending);
            return (
              <li key={c.name} role="option" aria-selected={c.active}>
                <button
                  type="button"
                  className={cn(
                    "flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground",
                    c.active && "font-medium",
                  )}
                  title={badge.title}
                  onClick={() => {
                    if (!c.active) switchContext.mutate(c.name);
                    setOpen(false);
                  }}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <Check
                      className={cn("h-4 w-4 shrink-0", c.active ? "opacity-100" : "opacity-0")}
                    />
                    <span className="truncate">{c.name}</span>
                  </span>
                  <Badge variant={badge.variant} className="shrink-0">
                    {badge.label}
                  </Badge>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
