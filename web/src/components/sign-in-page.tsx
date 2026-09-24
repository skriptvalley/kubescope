import { LogIn } from "lucide-react";
import { type FormEvent, useId, useRef, useState } from "react";

import wordmark from "@/assets/skriptvalley-wordmark.png";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useSignIn } from "@/hooks/use-session";
import { signInErrorMessage } from "@/lib/session";

// The session-mode sign-in page (ADR-0013), shown by AuthGate in place of the
// whole app while signed out. A password-only form unless the server was
// configured with a username (KUBESCOPE_AUTH_BASIC_USERNAME).

export function SignInPage({ usernameRequired }: { usernameRequired: boolean }) {
  const signIn = useSignIn();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [missing, setMissing] = useState(false);
  const passwordRef = useRef<HTMLInputElement>(null);
  const ids = { user: useId(), pass: useId(), err: useId(), title: useId() };

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (signIn.isPending) return;
    if (!password) {
      setMissing(true);
      passwordRef.current?.focus();
      return;
    }
    setMissing(false);
    signIn.mutate(usernameRequired ? { username, password } : { password }, {
      onError: () => {
        setPassword("");
        passwordRef.current?.focus();
      },
    });
  };

  const message = missing
    ? "Enter the password."
    : signIn.error
      ? signInErrorMessage(signIn.error, usernameRequired)
      : null;

  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-4" data-testid="sign-in-page">
      <Card className="w-full max-w-sm" aria-labelledby={ids.title}>
        <CardHeader className="space-y-3">
          <div className="flex items-center gap-2.5">
            <img src={wordmark} alt="Skript Valley" className="block h-[26px] w-auto" />
            <span className="h-5 w-px bg-border" aria-hidden="true" />
            <span className="font-display text-[15px] font-semibold tracking-[-0.01em]">Kubescope</span>
          </div>
          <h1 id={ids.title} className="font-display text-xl font-semibold leading-none tracking-tight">
            Sign in
          </h1>
          <CardDescription>This dashboard can change your cluster, so it's locked. Sign in to continue.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-3" onSubmit={onSubmit} noValidate>
            {usernameRequired && (
              <div className="space-y-1.5">
                <label htmlFor={ids.user} className="text-sm font-medium">
                  Username
                </label>
                <Input
                  id={ids.user}
                  autoComplete="username"
                  autoFocus
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  aria-describedby={message ? ids.err : undefined}
                />
              </div>
            )}
            <div className="space-y-1.5">
              <label htmlFor={ids.pass} className="text-sm font-medium">
                Password
              </label>
              <Input
                id={ids.pass}
                ref={passwordRef}
                type="password"
                autoComplete="current-password"
                autoFocus={!usernameRequired}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                aria-invalid={message ? true : undefined}
                aria-describedby={message ? ids.err : undefined}
              />
            </div>
            {message && (
              <p id={ids.err} role="alert" className="text-sm text-destructive">
                {message}
              </p>
            )}
            <Button type="submit" className="w-full" aria-disabled={signIn.isPending}>
              <LogIn />
              {signIn.isPending ? "Signing in…" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
