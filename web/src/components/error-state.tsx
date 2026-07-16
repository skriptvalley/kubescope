import { AlertCircle, RefreshCw } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api";
import { errorTitle } from "@/lib/error-title";

// Shared error state (Sprint 7). Every list and detail funnels its failures here
// so the ApiError code→title mapping lives in one place and every error offers a
// retry action, instead of the per-file ListError/DetailError/NodesError copies.

export function ErrorState({
  error,
  onRetry,
  title,
}: {
  error: Error;
  /** When given, renders a Retry button; wire it to the query's refetch. */
  onRetry?: () => void;
  /** Overrides the derived title (also the fallback for unmapped codes). */
  title?: string;
}) {
  const apiError = error instanceof ApiError ? error : undefined;
  const heading = errorTitle(error, title ?? "Something went wrong");
  const detail = apiError ? `${apiError.message} (${apiError.code})` : error.message;
  const guidance = apiError?.guidance;

  return (
    <Alert variant="destructive" data-testid="error-state">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>{heading}</AlertTitle>
      <AlertDescription>
        <div className="space-y-2">
          <p className="break-words">{detail}</p>
          {guidance && <p className="text-xs opacity-90">{guidance}</p>}
          {onRetry && (
            <Button variant="outline" size="sm" onClick={onRetry}>
              <RefreshCw className="h-3.5 w-3.5" />
              Retry
            </Button>
          )}
        </div>
      </AlertDescription>
    </Alert>
  );
}
