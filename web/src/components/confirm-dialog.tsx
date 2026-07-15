import { AlertCircle } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api";
import { cn } from "@/lib/utils";

export interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description?: React.ReactNode;
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

/** A modal confirmation gate every mutation passes through. For destructive
 *  actions a typed-name variant requires the user to retype the exact object
 *  name before Confirm enables, so a click alone cannot delete or drain. */
export function ConfirmDialog({
  open,
  title,
  description,
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
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      onClick={() => !pending && onCancel()}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${inputId}-title`}
        className="w-full max-w-md rounded-lg border bg-background p-6 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id={`${inputId}-title`} className="text-lg font-semibold">
          {title}
        </h2>
        {description && (
          <div className="mt-2 text-sm text-muted-foreground">{description}</div>
        )}

        {confirmText !== undefined && (
          <div className="mt-4 space-y-1.5">
            <label htmlFor={inputId} className="text-sm font-medium">
              {confirmTextLabel ?? `Type ${confirmText} to confirm`}
            </label>
            <Input
              id={inputId}
              value={typed}
              autoComplete="off"
              spellCheck={false}
              disabled={pending}
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

        <div className="mt-6 flex justify-end gap-2">
          <Button ref={cancelRef} variant="outline" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant={destructive ? "destructive" : "default"}
            onClick={onConfirm}
            disabled={!canConfirm}
            className={cn(pending && "opacity-70")}
          >
            {pending ? "Working…" : confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

function errorMessage(error: Error): string {
  return error instanceof ApiError ? `${error.message} (${error.code})` : error.message;
}
