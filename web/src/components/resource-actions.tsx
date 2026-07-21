import { RotateCw, Scale as ScaleIcon, Trash2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useDeleteResource,
  useRestartWorkload,
  useScaleWorkload,
} from "@/hooks/use-mutations";
import type { ResourceRef } from "@/lib/api";

// Mutation controls shared by the detail views. Each opens a confirmation dialog
// before acting; delete uses the typed-name gate. All are rendered only when the
// server is writable — read-only mode hides them (and the server rejects the
// call regardless, ADR-0005).

/** Delete button + typed-name confirmation. On success navigates to the kind's
 *  list, since the object being viewed no longer exists. `cascade` supplies the
 *  optional destructive-tinted warning box (namespace deletes). */
export function DeleteResourceButton({
  refx,
  kind,
  label = "Delete",
  cascade,
}: {
  refx: ResourceRef;
  kind: string;
  label?: string;
  cascade?: ReactNode;
}) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const del = useDeleteResource(refx, () =>
    navigate(`/resources/${refx.group}/${refx.version}/${refx.resource}`),
  );

  return (
    <>
      {/* Dusk: tinted destructive control, not a solid red button. */}
      <Button
        variant="ghost"
        size="sm"
        className="bg-destructive/10 text-destructive hover:bg-destructive/20"
        onClick={() => setOpen(true)}
      >
        <Trash2 /> {label}
      </Button>
      <ConfirmDialog
        open={open}
        destructive
        title={`Delete ${kind} ${refx.name}?`}
        description={
          <>
            This permanently deletes the {kind}
            {refx.namespace ? ` in namespace ${refx.namespace}` : ""}. This cannot be undone.
          </>
        }
        cascade={cascade}
        confirmText={refx.name}
        confirmTextLabel={`Type the ${kind.toLowerCase()} name to confirm`}
        confirmLabel={label}
        pending={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate()}
        onCancel={() => setOpen(false)}
      />
    </>
  );
}

/** Compact delete for a list row: an icon button + typed-name confirmation. The
 *  deleted row disappears via the list-cache invalidation in useDeleteResource;
 *  no navigation, since the surrounding list stays. */
export function DeleteRowButton({ refx, kind }: { refx: ResourceRef; kind: string }) {
  const [open, setOpen] = useState(false);
  const del = useDeleteResource(refx);

  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        className="h-8 w-8 text-muted-foreground hover:text-destructive"
        onClick={() => setOpen(true)}
        aria-label={`Delete ${kind} ${refx.name}`}
      >
        <Trash2 />
      </Button>
      <ConfirmDialog
        open={open}
        destructive
        title={`Delete ${kind} ${refx.name}?`}
        description={
          <>
            This permanently deletes the {kind}
            {refx.namespace ? ` in namespace ${refx.namespace}` : ""}. This cannot be undone.
          </>
        }
        confirmText={refx.name}
        confirmTextLabel={`Type the ${kind.toLowerCase()} name to confirm`}
        confirmLabel="Delete"
        pending={del.isPending}
        error={del.error}
        onConfirm={() => del.mutate(undefined, { onSuccess: () => setOpen(false) })}
        onCancel={() => setOpen(false)}
      />
    </>
  );
}

/** Scale control: a replica input plus a confirmation before applying. */
export function ScaleControl({
  resource,
  namespace,
  name,
  current,
}: {
  resource: string;
  namespace: string;
  name: string;
  current: number;
}) {
  const [replicas, setReplicas] = useState(String(current));
  const [open, setOpen] = useState(false);
  const scale = useScaleWorkload(resource, namespace, name);
  const target = Number(replicas);
  const valid = Number.isInteger(target) && target >= 0;

  return (
    <div className="flex items-center gap-2">
      <label htmlFor={`scale-${name}`} className="text-sm text-muted-foreground">
        Replicas
      </label>
      <Input
        id={`scale-${name}`}
        type="number"
        min={0}
        value={replicas}
        onChange={(e) => setReplicas(e.target.value)}
        className="h-8 w-20"
      />
      <Button
        variant="outline"
        size="sm"
        disabled={!valid || target === current}
        onClick={() => setOpen(true)}
      >
        <ScaleIcon /> Scale
      </Button>
      <ConfirmDialog
        open={open}
        title={`Scale ${name} to ${target} replica${target === 1 ? "" : "s"}?`}
        description={`Currently ${current}. This changes the desired replica count immediately.`}
        confirmLabel="Scale"
        pending={scale.isPending}
        error={scale.error}
        onConfirm={() =>
          scale.mutate(target, {
            onSuccess: () => setOpen(false),
          })
        }
        onCancel={() => setOpen(false)}
      />
    </div>
  );
}

/** Rollout-restart button + confirmation. */
export function RestartButton({
  resource,
  namespace,
  name,
  kind,
}: {
  resource: string;
  namespace: string;
  name: string;
  kind: string;
}) {
  const [open, setOpen] = useState(false);
  const restart = useRestartWorkload(resource, namespace, name);

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <RotateCw /> Restart
      </Button>
      <ConfirmDialog
        open={open}
        title={`Rollout-restart ${kind} ${name}?`}
        description="This rolls out fresh pods by stamping the pod-template restart annotation, the same as `kubectl rollout restart`."
        confirmLabel="Restart"
        pending={restart.isPending}
        error={restart.error}
        onConfirm={() =>
          restart.mutate(undefined, {
            onSuccess: () => setOpen(false),
          })
        }
        onCancel={() => setOpen(false)}
      />
    </>
  );
}
