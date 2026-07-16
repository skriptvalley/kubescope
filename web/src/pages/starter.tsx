import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ExternalLink, FolderOpen, RefreshCw } from "lucide-react";
import { type ReactNode, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useContexts, useContextsHealth, useSwitchContext } from "@/hooks/use-contexts";
import { api, ApiError, type SetupState } from "@/lib/api";
import { healthBadge } from "@/lib/context-health";
import { cn } from "@/lib/utils";

// The full-page starter shown by the Layout gate when the server has no usable
// cluster yet (FB-6). It replaces the routed page while the header/sidebar stay
// mounted so the context switcher and set-kubeconfig control remain usable.

export function StarterPage({ state }: { state: SetupState }) {
  return (
    <div className="mx-auto max-w-2xl py-6" data-testid="starter-page">
      {state.state === "no_kubeconfig" && <NoKubeconfig state={state} />}
      {state.state === "no_contexts" && <NoContexts state={state} />}
      {state.state === "no_active_context" && <NoActiveContext state={state} />}
      {state.state === "active_unreachable" && <ActiveUnreachable state={state} />}
    </div>
  );
}

const ADR0004 =
  "https://github.com/skriptvalley/kubescope/blob/main/docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md";

function DocLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1 font-medium underline underline-offset-2"
    >
      {children}
      <ExternalLink className="h-3 w-3" />
    </a>
  );
}

function NoKubeconfig({ state }: { state: SetupState }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>No kubeconfig found</CardTitle>
        <CardDescription>
          Kubescope needs a kubeconfig — the file that names your clusters and how
          to authenticate to them — before it can show anything.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        {state.guidance && <p className="text-muted-foreground">{state.guidance}</p>}
        <div className="space-y-2">
          <p className="font-medium">Point Kubescope at a kubeconfig</p>
          <ul className="list-disc space-y-1 pl-5 text-muted-foreground">
            <li>
              Running the binary directly: set{" "}
              <code className="font-mono">KUBESCOPE_KUBECONFIG</code> to a readable
              file (defaults to <code className="font-mono">~/.kube/config</code>).
            </li>
            <li>
              Running the container: mount one in —{" "}
              <code className="font-mono">
                docker run -v ~/.kube/config:/kubeconfig:ro …
              </code>
            </li>
          </ul>
        </div>
        <div className="space-y-1 text-muted-foreground">
          <p className="font-medium text-foreground">Local-cluster caveats (ADR-0004)</p>
          <p>
            A local cluster's API server at <code className="font-mono">127.0.0.1</code>{" "}
            is the container itself from inside Docker; exec credential plugins and
            file-path certificates named by the kubeconfig must also be reachable
            where Kubescope runs.
          </p>
          <DocLink href={state.docURL ?? ADR0004}>Read the Docker/auth guide</DocLink>
        </div>
        <SetKubeconfigControl canSet={state.canSetKubeconfig} />
      </CardContent>
    </Card>
  );
}

function NoContexts({ state }: { state: SetupState }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Kubeconfig has no contexts</CardTitle>
        <CardDescription>
          The file at <code className="font-mono">{state.kubeconfigPath}</code> was
          found but defines no contexts, so there is no cluster to connect to.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        {state.guidance && <p className="text-muted-foreground">{state.guidance}</p>}
        <div className="space-y-2">
          <p className="font-medium">Add a context</p>
          <ul className="list-disc space-y-1 pl-5 text-muted-foreground">
            <li>
              <code className="font-mono">kind export kubeconfig</code>
            </li>
            <li>
              <code className="font-mono">aws eks update-kubeconfig --name &lt;cluster&gt;</code>
            </li>
            <li>
              <code className="font-mono">gcloud container clusters get-credentials &lt;cluster&gt;</code>
            </li>
          </ul>
        </div>
        <SetKubeconfigControl canSet={state.canSetKubeconfig} />
      </CardContent>
    </Card>
  );
}

function NoActiveContext({ state }: { state: SetupState }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Pick a context</CardTitle>
        <CardDescription>
          {state.guidance ??
            "The kubeconfig has no current-context — pick a context to continue."}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <ContextChooser />
        <SetKubeconfigControl canSet={state.canSetKubeconfig} />
      </CardContent>
    </Card>
  );
}

function ActiveUnreachable({ state }: { state: SetupState }) {
  const queryClient = useQueryClient();
  const retry = () => {
    void queryClient.invalidateQueries({ queryKey: ["setup"] });
    void queryClient.invalidateQueries({ queryKey: ["contexts", "health"] });
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <AlertTriangle className="h-5 w-5 text-destructive" />
          Cannot reach the cluster
        </CardTitle>
        <CardDescription>
          {state.activeContext ? `Context ${state.activeContext} ` : "The active context "}
          could not be reached{state.reason ? ` (${state.reason})` : ""}.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        {state.message && <p className="break-words text-muted-foreground">{state.message}</p>}
        {state.guidance && <p className="text-muted-foreground">{state.guidance}</p>}
        {state.docURL && <DocLink href={state.docURL}>Learn more</DocLink>}
        <div>
          <Button variant="outline" size="sm" onClick={retry}>
            <RefreshCw className="h-3.5 w-3.5" />
            Retry
          </Button>
        </div>
        <div className="space-y-2">
          <p className="font-medium">Switch to another context</p>
          <ContextChooser />
        </div>
        <SetKubeconfigControl canSet={state.canSetKubeconfig} />
      </CardContent>
    </Card>
  );
}

/** The kubeconfig context list with health badges; one click selects a context
 *  (reuses the shared switch mutation so every cluster cache refetches). */
function ContextChooser() {
  const { data: contexts, isPending } = useContexts();
  const { data: health, isPending: healthPending } = useContextsHealth();
  const switchContext = useSwitchContext();

  if (isPending) return <p className="text-muted-foreground">Loading contexts…</p>;
  if (!contexts || contexts.length === 0) {
    return <p className="text-muted-foreground">No contexts to choose from.</p>;
  }

  const healthByName = new Map((health ?? []).map((h) => [h.name, h]));

  return (
    <ul className="divide-y rounded-md border" data-testid="context-chooser">
      {contexts.map((c) => {
        const badge = healthBadge(healthByName.get(c.name), healthPending);
        return (
          <li key={c.name}>
            <button
              type="button"
              disabled={switchContext.isPending}
              className={cn(
                "flex w-full items-center justify-between gap-2 px-3 py-2 text-left hover:bg-accent hover:text-accent-foreground disabled:opacity-50",
                c.active && "font-medium",
              )}
              onClick={() => {
                if (!c.active) switchContext.mutate(c.name);
              }}
            >
              <span className="truncate">{c.name}</span>
              <Badge variant={badge.variant} className="shrink-0" title={badge.title}>
                {badge.label}
              </Badge>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/** Absolute-path input that repoints the server at another kubeconfig at runtime
 *  (ADR-0007). Hidden behind a flag: when disabled, only a hint is shown. */
function SetKubeconfigControl({ canSet }: { canSet: boolean }) {
  const queryClient = useQueryClient();
  const [path, setPath] = useState("");
  const mutation = useMutation({
    mutationFn: (p: string) => api.setup.setKubeconfig(p),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["setup"] });
      void queryClient.invalidateQueries({ queryKey: ["contexts"] });
      void queryClient.invalidateQueries({ queryKey: ["contexts", "health"] });
    },
  });

  if (!canSet) {
    return (
      <p className="text-xs text-muted-foreground" data-testid="set-kubeconfig-disabled">
        Enable pointing Kubescope at another kubeconfig with{" "}
        <code className="font-mono">KUBESCOPE_ALLOW_KUBECONFIG_SET=true</code>{" "}
        (read-only mode keeps this off) — or mount/point a kubeconfig and restart.
      </p>
    );
  }

  const apiError = mutation.error instanceof ApiError ? mutation.error : undefined;
  const trimmed = path.trim();

  return (
    <form
      className="space-y-2 border-t pt-4"
      data-testid="set-kubeconfig-form"
      onSubmit={(e) => {
        e.preventDefault();
        if (trimmed) mutation.mutate(trimmed);
      }}
    >
      <label className="flex items-center gap-2 text-sm font-medium">
        <FolderOpen className="h-4 w-4" />
        Use a different kubeconfig
      </label>
      <div className="flex gap-2">
        <Input
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/kubeconfig"
          aria-label="Absolute kubeconfig path"
          spellCheck={false}
        />
        <Button type="submit" size="sm" disabled={!trimmed || mutation.isPending}>
          {mutation.isPending ? "Loading…" : "Use"}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Absolute path readable by the Kubescope process (in Docker, a mounted
        volume such as <code className="font-mono">/kubeconfig</code>).
      </p>
      {apiError && (
        <div role="alert" className="space-y-1 text-xs text-destructive">
          <p className="break-words">
            {apiError.message} ({apiError.code})
          </p>
          {apiError.guidance && <p className="opacity-90">{apiError.guidance}</p>}
        </div>
      )}
    </form>
  );
}
