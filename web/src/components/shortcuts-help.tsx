import { Keyboard } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";

// Keyboard shortcuts help (Story 7.5): a popover documenting the app's keyboard
// navigation. Opened by the header button or the "?" key.

const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: ["/"], label: "Focus search" },
  { keys: ["↑", "↓"], label: "Move selection in search results" },
  { keys: ["Enter"], label: "Open the selected result" },
  { keys: ["Esc"], label: "Close a dialog, panel, or search" },
  { keys: ["?"], label: "Toggle this help" },
];

/** Whether focus is in a field where "?" should type, not open help. */
function inEditable(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
}

export function ShortcutsHelp() {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);

  // "?" toggles help; Escape closes it (returning focus to the trigger).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "?" && !inEditable(e.target)) {
        e.preventDefault();
        setOpen((o) => !o);
      } else if (e.key === "Escape" && open) {
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  return (
    <div ref={containerRef} className="relative">
      <Button
        ref={triggerRef}
        variant="ghost"
        size="icon"
        aria-label="Keyboard shortcuts"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <Keyboard className="h-4 w-4" />
      </Button>

      {open && (
        <div
          role="dialog"
          aria-label="Keyboard shortcuts"
          className="absolute right-0 z-20 mt-1 w-72 rounded-md border bg-popover p-3 shadow-md"
        >
          <h3 className="mb-2 text-sm font-semibold">Keyboard shortcuts</h3>
          <ul className="space-y-1.5">
            {SHORTCUTS.map((s) => (
              <li key={s.label} className="flex items-center justify-between gap-3 text-sm">
                <span className="text-muted-foreground">{s.label}</span>
                <span className="flex shrink-0 gap-1">
                  {s.keys.map((k) => (
                    <kbd
                      key={k}
                      className="rounded border bg-muted px-1.5 py-0.5 font-mono text-xs"
                    >
                      {k}
                    </kbd>
                  ))}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
