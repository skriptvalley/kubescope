import { useMutation } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { api, ApiError, type KubeObject } from "@/lib/api";

// Secret data panel (ADR-0005): keys are listed, values masked by default, and
// revealed one key at a time on explicit click. The masked value never leaves
// the server on the object fetch; reveal is a separate request for a single key,
// so nothing is decoded until the user asks for it.

/** A single Secret key row with reveal-on-click. */
function SecretRow({
  namespace,
  name,
  keyName,
}: {
  namespace: string;
  name: string;
  keyName: string;
}) {
  const [shown, setShown] = useState(false);
  const reveal = useMutation({
    mutationFn: () => api.secrets.reveal(namespace, name, keyName),
    onSuccess: () => setShown(true),
  });

  const hide = () => setShown(false);

  return (
    <div className="grid grid-cols-1 gap-1 rounded-md border p-3 sm:grid-cols-[minmax(0,14rem)_1fr] sm:items-center sm:gap-3">
      <code className="truncate font-mono text-xs font-medium" title={keyName}>
        {keyName}
      </code>
      <div className="flex min-w-0 items-center gap-2">
        {shown && reveal.data !== undefined ? (
          <pre
            data-testid={`secret-value-${keyName}`}
            className="min-w-0 flex-1 overflow-x-auto whitespace-pre-wrap break-all rounded bg-muted/60 px-2 py-1 font-mono text-xs"
          >
            {reveal.data}
          </pre>
        ) : (
          <span className="flex-1 select-none font-mono text-xs tracking-widest text-muted-foreground">
            ••••••••
          </span>
        )}
        {shown ? (
          <Button variant="ghost" size="sm" onClick={hide} aria-label={`Hide ${keyName}`}>
            <EyeOff /> Hide
          </Button>
        ) : (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => reveal.mutate()}
            disabled={reveal.isPending}
            aria-label={`Reveal ${keyName}`}
          >
            <Eye /> {reveal.isPending ? "Revealing…" : "Reveal"}
          </Button>
        )}
        {reveal.isError && (
          <span className="text-xs text-destructive">
            {reveal.error instanceof ApiError ? reveal.error.message : "Failed"}
          </span>
        )}
      </div>
    </div>
  );
}

/** Renders a Secret's data keys (masked) with per-key reveal. Keys come from the
 *  masked object the detail page already fetched — the values there are
 *  redacted, so only the key names are read here. */
export function SecretDetail({
  object,
  namespace,
  name,
}: {
  object: KubeObject;
  namespace: string;
  name: string;
}) {
  const data = (object.data ?? {}) as Record<string, unknown>;
  const keys = Object.keys(data).sort();
  const type = (object.type as string) ?? "Opaque";

  return (
    <section className="space-y-2" aria-label="Secret data">
      <div className="flex items-center gap-2">
        <h3 className="text-sm font-semibold">Data</h3>
        <Badge variant="secondary" className="font-normal">
          {type}
        </Badge>
      </div>
      {keys.length === 0 ? (
        <p className="text-sm text-muted-foreground">This secret has no data.</p>
      ) : (
        <div className="space-y-2">
          {keys.map((k) => (
            <SecretRow key={k} namespace={namespace} name={name} keyName={k} />
          ))}
        </div>
      )}
    </section>
  );
}
