import { LogIn } from "lucide-react";
import { type FormEvent, useState } from "react";

import wordmark from "@/assets/skriptvalley-wordmark.png";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useSignIn } from "@/hooks/use-session";
import { ApiError } from "@/lib/api";

// The session-mode sign-in page (ADR-0013), shown by AuthGate in place of the
// whole app while signed out. A password-only form unless the server was
// configured with a username (KUBESCOPE_AUTH_BASIC_USERNAME).

export function SignInPage({ usernameRequired }: { usernameRequired: boolean }) {
  const signIn = useSignIn();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!password || signIn.isPending) return;
    signIn.mutate(usernameRequired ? { username, password } : { password }, {
      onError: () => setPassword(""),
    });
  };

  const error = signIn.error;
  const message =
    error instanceof ApiError && error.status === 401
      ? usernameRequired
        ? "Wrong username or password."
        : "Wrong password."
      : error
        ? "Sign-in failed — the server could not be reached."
        : null;

  return (
    <div className="flex min-h-screen items-center justify-center bg-background px-4" data-testid="sign-in-page">
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-3">
          <div className="flex items-center gap-2.5">
            <img src={wordmark} alt="Skript Valley" className="block h-[26px] w-auto" />
            <span className="h-5 w-px bg-border" aria-hidden="true" />
            <span className="font-display text-[15px] font-semibold tracking-[-0.01em]">Kubescope</span>
          </div>
          <CardTitle className="font-display text-xl">Sign in</CardTitle>
          <CardDescription>This dashboard can change your cluster, so it's locked. Sign in to continue.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-3" onSubmit={onSubmit}>
            {usernameRequired && (
              <Input
                aria-label="Username"
                placeholder="Username"
                autoComplete="username"
                autoFocus
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            )}
            <Input
              aria-label="Password"
              type="password"
              placeholder="Password"
              autoComplete="current-password"
              autoFocus={!usernameRequired}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            {message && (
              <p role="alert" className="text-sm text-destructive">
                {message}
              </p>
            )}
            <Button type="submit" className="w-full" disabled={!password || signIn.isPending}>
              <LogIn />
              {signIn.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
