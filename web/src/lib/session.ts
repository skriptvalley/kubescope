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

/** Thrown when the server accepted the password but the browser didn't keep the
 *  session cookie (blocked cookies, or a stray same-named cookie shadowing it). */
export class CookieNotKeptError extends Error {
  constructor() {
    super("signed in, but the browser did not keep the session cookie");
    this.name = "CookieNotKeptError";
  }
}

/** A human reason for a failed sign-in, by what actually went wrong. */
export function signInErrorMessage(error: unknown, usernameRequired: boolean): string {
  if (error instanceof CookieNotKeptError) {
    return "The server accepted the password, but your browser didn't keep the session cookie. Allow cookies for this site (or clear old kubescope_session cookies) and try again.";
  }
  if (error instanceof ApiError) {
    if (error.status === 401) return usernameRequired ? "Wrong username or password." : "Wrong password.";
    if (error.status === 429) return "Too many failed sign-ins — wait a minute, then try again.";
    if (error.status === 403) return "The request was blocked (cross-origin). Open Kubescope from its own address.";
    if (error.status >= 500) return "The server hit an error — try again shortly.";
    return `Sign-in failed: ${error.message}`;
  }
  return "Couldn't reach the Kubescope server — check the connection and try again.";
}
