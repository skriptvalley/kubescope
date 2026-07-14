import { Check, ChevronsUpDown } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  useContexts,
  useContextsHealth,
  useSwitchContext,
} from "@/hooks/use-contexts";
import { ApiError } from "@/lib/api";
import { healthBadge } from "@/lib/context-health";
import { cn } from "@/lib/utils";

export function ContextSwitcher() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const { data: contexts, isPending, isError } = useContexts();
  const { data: health, isPending: healthPending } = useContextsHealth();
  const switchContext = useSwitchContext();

  // Close on Escape (restoring focus to the trigger) or an outside click.
  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
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

  function choose(name: string, isActive: boolean) {
    if (!isActive) switchContext.mutate(name);
    setOpen(false);
    triggerRef.current?.focus();
  }

  return (
    <div className="relative" ref={ref}>
      <Button
        ref={triggerRef}
        variant="outline"
        size="sm"
        className="min-w-52 justify-between"
        onClick={() => setOpen((o) => !o)}
        disabled={isPending || switchContext.isPending}
        aria-haspopup="true"
        aria-expanded={open}
        aria-label="Switch context"
      >
        <span className="truncate">{label}</span>
        <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
      </Button>
      {open && contexts && contexts.length > 0 && (
        <ul className="absolute right-0 z-50 mt-1 max-h-80 w-72 overflow-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
          {contexts.map((c) => {
            const badge = healthBadge(healthByName.get(c.name), healthPending);
            return (
              <li key={c.name}>
                <button
                  type="button"
                  className={cn(
                    "flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-accent hover:text-accent-foreground",
                    c.active && "font-medium",
                  )}
                  title={badge.title}
                  aria-current={c.active ? "true" : undefined}
                  onClick={() => choose(c.name, c.active)}
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
      {switchContext.isError && (
        <p role="alert" className="absolute right-0 mt-1 w-72 rounded-md border border-destructive bg-background p-2 text-xs text-destructive shadow-md">
          Switch failed: {switchError(switchContext.error)}
        </p>
      )}
    </div>
  );
}

function switchError(error: unknown): string {
  if (error instanceof ApiError) return `${error.message} (${error.code})`;
  return error instanceof Error ? error.message : "unknown error";
}
