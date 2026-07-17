import { FolderOpen, RefreshCw, Trash2, X } from "lucide-react";
import { useEffect, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useAddKubeconfigSource,
  useKubeconfigSources,
  useRemoveKubeconfigSource,
  useRescanKubeconfigSources,
} from "@/hooks/use-kubeconfigs";
import { ApiError, type KubeconfigSource, type KubeconfigSourceFile } from "@/lib/api";

// The kubeconfig source registry surface (FB-8). One component reused by the
// full-page starter (pre-ready) and the context switcher's "Manage kubeconfig
// sources" dialog (in-session). All server state flows through the registry
// hooks; canSetKubeconfig comes from the listing (the server folds in the flag
// and read-only mode), so the add/remove controls gate on the fetched data.

type BadgeVariant = "default" | "secondary" | "destructive" | "outline";

function sourceStatusVariant(status: KubeconfigSource["status"]): BadgeVariant {
  if (status === "ok") return "default";
  if (status === "empty") return "secondary";
  return "destructive"; // missing | unparseable
}

function fileStatusVariant(status: KubeconfigSourceFile["status"]): BadgeVariant {
  if (status === "ok") return "default";
  if (status === "unparseable") return "destructive";
  return "secondary"; // too_large | hidden
}

export function KubeconfigSources() {
  const { data, isPending, isError, error } = useKubeconfigSources();
  const rescan = useRescanKubeconfigSources();

  if (isPending) {
    return <p className="text-sm text-muted-foreground">Loading kubeconfig sources…</p>;
  }
  if (isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {error instanceof ApiError ? error.message : "Failed to load kubeconfig sources"}
      </p>
    );
  }

  const canSet = data.canSetKubeconfig;

  return (
    <div className="space-y-4" data-testid="kubeconfig-sources">
      <div className="flex items-center justify-between">
        <p className="text-sm font-medium">Kubeconfig sources</p>
        <Button variant="outline" size="sm" onClick={rescan}>
          <RefreshCw className="h-3.5 w-3.5" />
          Rescan
        </Button>
      </div>

      {data.sources.length === 0 ? (
        <p className="text-sm text-muted-foreground">No kubeconfig sources registered.</p>
      ) : (
        <ul className="divide-y rounded-md border" data-testid="kubeconfig-source-list">
          {data.sources.map((source) => (
            <SourceRow key={source.id} source={source} canRemove={canSet} />
          ))}
        </ul>
      )}

      {canSet ? <AddSourceForm /> : <DisabledHint />}
    </div>
  );
}

function SourceRow({ source, canRemove }: { source: KubeconfigSource; canRemove: boolean }) {
  const remove = useRemoveKubeconfigSource();
  const removeError = remove.error instanceof ApiError ? remove.error : undefined;

  return (
    <li className="space-y-2 p-3" data-testid="kubeconfig-source">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <code className="break-all font-mono text-sm">{source.path}</code>
            <Badge variant="outline" className="shrink-0">
              {source.kind}
            </Badge>
            <Badge variant={sourceStatusVariant(source.status)} className="shrink-0">
              {source.status}
            </Badge>
            <span className="text-xs text-muted-foreground">{source.origin}</span>
          </div>
          {source.message && (
            <p className="break-words text-xs text-muted-foreground">{source.message}</p>
          )}
          {source.shadowed && source.shadowed.length > 0 && (
            <p className="text-xs text-muted-foreground">
              shadowed by an earlier source: {source.shadowed.join(", ")}
            </p>
          )}
        </div>
        {canRemove && (
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Remove ${source.path}`}
            disabled={remove.isPending}
            onClick={() => remove.mutate(source.id)}
          >
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        )}
      </div>

      {source.files && source.files.length > 0 && (
        <ul className="space-y-1 border-l pl-3">
          {source.files.map((file) => (
            <li key={file.path} className="space-y-0.5" data-testid="kubeconfig-source-file">
              <div className="flex flex-wrap items-center gap-2">
                <code className="break-all font-mono text-xs">{file.path}</code>
                <Badge variant={fileStatusVariant(file.status)} className="shrink-0">
                  {file.status}
                </Badge>
              </div>
              {file.message && (
                <p className="break-words text-xs text-muted-foreground">{file.message}</p>
              )}
              {file.shadowed && file.shadowed.length > 0 && (
                <p className="text-xs text-muted-foreground">
                  shadowed by an earlier source: {file.shadowed.join(", ")}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}

      {removeError && (
        <p role="alert" className="text-xs text-destructive">
          {removeError.message} ({removeError.code})
        </p>
      )}
    </li>
  );
}

function AddSourceForm() {
  const add = useAddKubeconfigSource();
  const [path, setPath] = useState("");
  const trimmed = path.trim();
  const apiError = add.error instanceof ApiError ? add.error : undefined;

  return (
    <form
      className="space-y-2 border-t pt-4"
      data-testid="add-kubeconfig-source-form"
      onSubmit={(e) => {
        e.preventDefault();
        if (trimmed) add.mutate(trimmed, { onSuccess: () => setPath("") });
      }}
    >
      <label className="flex items-center gap-2 text-sm font-medium">
        <FolderOpen className="h-4 w-4" />
        Add a kubeconfig source
      </label>
      <div className="flex gap-2">
        <Input
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/kubeconfig"
          aria-label="Absolute kubeconfig path"
          spellCheck={false}
        />
        <Button type="submit" size="sm" disabled={!trimmed || add.isPending}>
          {add.isPending ? "Adding…" : "Add"}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Absolute path to a kubeconfig file or a directory of them, readable by the
        Kubescope process (in Docker, a mounted volume such as{" "}
        <code className="font-mono">/kubeconfig</code>).
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

function DisabledHint() {
  return (
    <p className="text-xs text-muted-foreground" data-testid="kubeconfig-sources-disabled">
      Enable adding or removing kubeconfig sources with{" "}
      <code className="font-mono">KUBESCOPE_ALLOW_KUBECONFIG_SET=true</code>{" "}
      (read-only mode keeps this off) — or mount/point a kubeconfig and restart.
    </p>
  );
}

/** Modal wrapper hosting the registry surface, opened from the context switcher's
 *  "Manage kubeconfig sources" entry (the in-session counterpart to the starter). */
export function KubeconfigSourcesDialog({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Manage kubeconfig sources"
        className="max-h-[85vh] w-full max-w-2xl overflow-y-auto rounded-lg border bg-background p-6 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Manage kubeconfig sources</h2>
          <Button variant="ghost" size="icon" aria-label="Close" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </div>
        <KubeconfigSources />
      </div>
    </div>
  );
}
