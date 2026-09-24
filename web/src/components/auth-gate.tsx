import { type ReactNode } from "react";

import { ErrorState } from "@/components/error-state";
import { SignInPage } from "@/components/sign-in-page";
import { useSession } from "@/hooks/use-session";

/** Gates the whole app on the session sign-in (ADR-0013). In `session` mode a
 *  signed-out visitor sees only the sign-in page — no layout, no cluster calls.
 *  `none` and `basic` modes render straight through (Basic is enforced by the
 *  browser before the SPA loads). An expired session anywhere in the app flips
 *  the cached state (lib/session) and lands back here. */
export function AuthGate({ children }: { children: ReactNode }) {
  const session = useSession();

  if (session.isPending) {
    return <div className="min-h-screen bg-background" data-testid="auth-pending" />;
  }
  if (session.isError) {
    return (
      <div className="mx-auto max-w-xl px-4 py-16">
        <ErrorState
          error={session.error}
          onRetry={() => session.refetch()}
          title="Can't reach the Kubescope server"
        />
      </div>
    );
  }
  if (session.data.mode === "session" && !session.data.authenticated) {
    return <SignInPage usernameRequired={session.data.usernameRequired} />;
  }
  return <>{children}</>;
}
