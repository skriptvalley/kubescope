import { Check, ChevronsUpDown, Settings } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { KubeconfigSourcesDialog } from "@/components/kubeconfig-sources";
import {
  useContexts,
  useContextsHealth,
  useSwitchContext,
} from "@/hooks/use-contexts";
import { useSetupState } from "@/hooks/use-setup";
import { ApiError } from "@/lib/api";
import { healthTone } from "@/lib/context-health";
import { toneStyles } from "@/lib/tone-style";
import { cn } from "@/lib/utils";

export function ContextSwitcher() {
  const [open, setOpen] = useState(false);
  const [manageOpen, setManageOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const { data: contexts, isPending, isError } = useContexts();
  const { data: health, isPending: healthPending } = useContextsHealth();
  const { data: setup } = useSetupState();
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
    // FB-9: a genuine mid-session error (setup ready) stays red; before the
    // server has a usable cluster (any non-ready state, incl. loading) the
    // switcher is just idle, so show a neutral muted label instead of alarm.
    if (setup?.state === "ready") {
      return <span className="text-sm text-destructive">kubeconfig error</span>;
    }
    return <span className="text-sm text-muted-foreground">no cluster</span>;
  }

  const active = contexts?.find((c) => c.active);
  const healthByName = new Map((health ?? []).map((h) => [h.name, h]));
  const canManage = setup?.canSetKubeconfig ?? false;

  const activeTone = active ? healthTone(healthByName.get(active.name), healthPending).tone : "neutral";
  const dotClass = toneStyles[activeTone].dot;

  const label = switchContext.isPending
    ? "Switching…"
    : (active?.name ?? (isPending ? "Loading…" : "Select context"));

  function choose(name: string, isActive: boolean) {
    if (!isActive) switchContext.mutate(name);
    setOpen(false);
    triggerRef.current?.focus();
  }

  return (
    <div className="relative shrink-0" ref={ref}>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((o) => !o)}
        disabled={isPending || switchContext.isPending}
        aria-haspopup="true"
        aria-expanded={open}
        aria-label="Switch context"
        className="inline-flex h-8 min-w-[200px] items-center justify-between gap-1.5 rounded-md border bg-background px-2.5 text-[13px] font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-60"
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className={cn("h-[7px] w-[7px] shrink-0 rounded-full", dotClass)} aria-hidden="true" />
          <span className="truncate font-mono text-[12.5px]">{label}</span>
        </span>
        <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 opacity-50" />
      </button>
      {open && contexts && contexts.length > 0 && (
        <ul className="absolute left-0 z-50 mt-1.5 w-72 animate-fadeIn overflow-auto rounded-lg border bg-popover p-1 text-popover-foreground shadow-popover">
          <li className="px-2 pb-1 pt-1.5 text-[11px] font-medium uppercase tracking-[0.06em] text-muted-foreground">
            Contexts
          </li>
          {contexts.map((c) => {
            const { tone, label: badgeLabel, title } = healthTone(healthByName.get(c.name), healthPending);
            return (
              <li key={c.name}>
                <button
                  type="button"
                  className={cn(
                    "flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-[13px] hover:bg-muted",
                    c.active && "font-medium",
                  )}
                  title={title}
                  aria-current={c.active ? "true" : undefined}
                  onClick={() => choose(c.name, c.active)}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <Check
                      className={cn("h-3.5 w-3.5 shrink-0 text-primary", c.active ? "opacity-100" : "opacity-0")}
                    />
                    <span className="truncate font-mono text-[12.5px]">{c.name}</span>
                  </span>
                  <span
                    className={cn(
                      "shrink-0 rounded-sm px-1.5 py-0.5 text-xs font-medium",
                      toneStyles[tone].pill,
                    )}
                  >
                    {badgeLabel}
                  </span>
                </button>
              </li>
            );
          })}
          {canManage && (
            <>
              <li role="separator" aria-hidden="true" className="mx-1 my-1 h-px bg-border" />
              <li>
                <button
                  type="button"
                  className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-[13px] text-muted-foreground hover:bg-muted hover:text-foreground"
                  onClick={() => {
                    setOpen(false);
                    setManageOpen(true);
                  }}
                >
                  <Settings className="h-3.5 w-3.5 shrink-0 opacity-70" />
                  Manage kubeconfig sources
                </button>
              </li>
            </>
          )}
        </ul>
      )}
      {switchContext.isError && (
        <p role="alert" className="absolute left-0 mt-1.5 w-72 rounded-md border border-destructive bg-background p-2 text-xs text-destructive shadow-popover">
          Switch failed: {switchError(switchContext.error)}
        </p>
      )}
      {manageOpen && <KubeconfigSourcesDialog onClose={() => setManageOpen(false)} />}
    </div>
  );
}

function switchError(error: unknown): string {
  if (error instanceof ApiError) return `${error.message} (${error.code})`;
  return error instanceof Error ? error.message : "unknown error";
}
