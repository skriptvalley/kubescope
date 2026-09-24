// Session sign-in plumbing shared by the query client and the auth gate
// (ADR-0013). When the server answers any API call with 401 "unauthenticated"
// (the session expired, or the cookie was cleared), the cached sign-in state is
// flipped to signed-out, which makes the AuthGate swap the app for the sign-in
// page — no per-query handling needed.

import type { QueryClient } from "@tanstack/react-query";

import { ApiError, type SessionState } from "@/lib/api";

export const sessionQueryKey = ["session"] as const;

/** True for the server's "sign in required" answer (not a wrong password). */
export function isUnauthenticated(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401 && error.code === "unauthenticated";
}

/** Marks the cached session signed-out after an unauthenticated API answer. */
export function handleAuthError(queryClient: QueryClient, error: unknown): void {
  if (!isUnauthenticated(error)) return;
  queryClient.setQueryData<SessionState>(sessionQueryKey, (prev) =>
    prev ? { ...prev, authenticated: false } : prev,
  );
}

/** Query retry policy: one retry, but never for a sign-in-required answer. */
export function retryUnlessUnauthenticated(failureCount: number, error: unknown): boolean {
  return !isUnauthenticated(error) && failureCount < 1;
}
