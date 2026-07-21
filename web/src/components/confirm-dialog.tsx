import { AlertCircle } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api";
import { cn } from "@/lib/utils";

export interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description?: React.ReactNode;
  /** Optional destructive-tinted warning box (e.g. namespace cascade delete),
   *  rendered between the description and the typed-name gate. */
  cascade?: React.ReactNode;
  confirmLabel?: string;
  /** Destructive styling for irreversible actions (delete/drain). */
  destructive?: boolean;
  /** When set, the user must type this exact string to enable confirmation —
   *  the typed-name gate for delete and drain. */
  confirmText?: string;
  /** Label above the typed-name input (e.g. "Type the resource name to confirm"). */
  confirmTextLabel?: string;
  pending?: boolean;
  /** An error from the attempted action, surfaced in-dialog. */
  error?: Error | null;
  onConfirm: () => void;
  onCancel: () => void;
}

/** A modal confirmation gate every mutation passes through (Dusk dialog). For
 *  destructive actions a typed-name variant requires the user to retype the exact
 *  object name before Confirm enables, so a click alone cannot delete or drain. */
export function ConfirmDialog({
  open,
  title,
  description,
  cascade,
  confirmLabel = "Confirm",
  destructive = false,
  confirmText,
  confirmTextLabel,
  pending = false,
  error,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const [typed, setTyped] = useState("");
  const inputId = useId();
  const cancelRef = useRef<HTMLButtonElement>(null);

  // Reset the typed value whenever the dialog opens so a prior attempt's text
  // never carries over into a fresh confirmation.
  useEffect(() => {
    if (open) setTyped("");
  }, [open]);

  // Focus the safe (cancel) control on open; Escape cancels.
  useEffect(() => {
    if (!open) return;
    cancelRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !pending) onCancel();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, pending, onCancel]);

  if (!open) return null;

  const nameGateOk = confirmText === undefined || typed === confirmText;
  const canConfirm = nameGateOk && !pending;

  return (
    <>
      <div
        className="fixed inset-0 z-50 animate-fadeIn bg-black/50"
        onClick={() => !pending && onCancel()}
        aria-hidden="true"
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${inputId}-title`}
        className="fixed left-1/2 top-1/2 z-50 w-[calc(100%-32px)] max-w-[448px] -translate-x-1/2 -translate-y-1/2 animate-dlgIn rounded-lg border bg-popover p-6 text-popover-foreground shadow-dialog"
      >
        <div className="flex flex-col gap-1">
          <h2 id={`${inputId}-title`} className="font-display text-base font-semibold">
            {title}
          </h2>
          {description && (
            <p className="text-[13.5px] leading-normal text-muted-foreground">{description}</p>
          )}
        </div>

        {cascade && (
          <div className="mt-3.5 flex gap-2 rounded-md border border-destructive/25 bg-destructive/[0.07] px-3 py-2.5 text-[12.5px] leading-normal">
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive" />
            <span>{cascade}</span>
          </div>
        )}

        {confirmText !== undefined && (
          <div className="mt-4 space-y-1.5">
            <label htmlFor={inputId} className="block text-[13px] font-medium">
              {confirmTextLabel ?? (
                <>
                  Type{" "}
                  <span className="rounded-sm bg-muted px-1.5 py-px font-mono text-xs">{confirmText}</span>{" "}
                  to confirm
                </>
              )}
            </label>
            <Input
              id={inputId}
              value={typed}
              autoComplete="off"
              spellCheck={false}
              disabled={pending}
              placeholder={confirmText}
              className={cn(
                "font-mono",
                destructive &&
                  "focus-visible:border-destructive focus-visible:ring-destructive/30",
              )}
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && canConfirm) onConfirm();
              }}
            />
          </div>
        )}

        {error && (
          <div className="mt-4 flex items-start gap-2 rounded-md border border-destructive/50 bg-destructive/5 p-3 text-sm text-destructive">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            <span data-testid="confirm-error">{errorMessage(error)}</span>
          </div>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button
            ref={cancelRef}
            type="button"
            onClick={onCancel}
            disabled={pending}
            className="inline-flex h-8 items-center justify-center rounded-md border bg-background px-3 text-[13.5px] font-medium transition-colors hover:bg-muted disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={!canConfirm}
            className={cn(
              "inline-flex h-8 items-center justify-center gap-1.5 rounded-md px-3 text-[13.5px] font-medium transition-colors disabled:pointer-events-none disabled:opacity-50",
              destructive
                ? "bg-destructive/10 text-destructive hover:bg-destructive/20"
                : "bg-primary text-primary-foreground hover:bg-primary/90",
              pending && "opacity-70",
            )}
          >
            {pending ? "Working…" : confirmLabel}
          </button>
        </div>
      </div>
    </>
  );
}

function errorMessage(error: Error): string {
  return error instanceof ApiError ? `${error.message} (${error.code})` : error.message;
}
