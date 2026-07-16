import type { ReactNode } from "react";

/** The shared empty-state for lists and detail panels. One consistent look for
 *  "nothing here", replacing the per-page inline paragraphs. */
export function EmptyState({ message, children }: { message: string; children?: ReactNode }) {
  return (
    <div className="py-8 text-center text-sm text-muted-foreground" data-testid="empty-state">
      <p>{message}</p>
      {children}
    </div>
  );
}
