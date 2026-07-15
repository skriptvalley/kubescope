import { AlertCircle, Pencil, RefreshCw } from "lucide-react";
import { useState } from "react";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { YamlEditor } from "@/components/yaml-editor";
import { YamlView } from "@/components/yaml-view";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useResourceYaml } from "@/hooks/use-resource";
import { useApplyResource } from "@/hooks/use-mutations";
import { ApiError, type ResourceRef } from "@/lib/api";

/** The YAML tab: read-only view by default, an in-place CodeMirror editor when
 *  editing. Applying goes through a confirmation dialog; a stale-resourceVersion
 *  409 is surfaced with a reload-and-retry path (never a silent overwrite), and
 *  invalid YAML / server validation errors are shown inline. The Edit affordance
 *  is hidden in read-only mode — though the server would reject the apply anyway. */
export function YamlTab({
  refx,
  kind,
  readOnly,
}: {
  refx: ResourceRef;
  kind: string;
  readOnly: boolean;
}) {
  const yaml = useResourceYaml(refx, true);
  const apply = useApplyResource(refx);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);

  const startEditing = () => {
    setDraft(yaml.data ?? "");
    apply.reset();
    setEditing(true);
  };

  const cancelEditing = () => {
    setEditing(false);
    apply.reset();
  };

  const doApply = () => {
    apply.mutate(draft, {
      onSuccess: () => {
        setConfirmOpen(false);
        setEditing(false);
      },
      // Keep the draft and drop to an inline error (validation) or the conflict
      // banner; the dialog closes either way so the message sits by the editor.
      onError: () => setConfirmOpen(false),
    });
  };

  const reloadLatest = async () => {
    const res = await yaml.refetch();
    if (res.data !== undefined) setDraft(res.data);
    apply.reset();
  };

  if (yaml.isPending) return <Skeleton className="h-64 w-full" data-testid="yaml-loading" />;
  if (yaml.isError) return <YamlError error={yaml.error} />;

  const applyError = apply.error instanceof ApiError ? apply.error : undefined;
  const isConflict = applyError?.code === "conflict";

  if (!editing) {
    return (
      <div className="space-y-3">
        {!readOnly && (
          <div className="flex justify-end">
            <Button variant="outline" size="sm" onClick={startEditing}>
              <Pencil /> Edit
            </Button>
          </div>
        )}
        <YamlView yaml={yaml.data} />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={cancelEditing} disabled={apply.isPending}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => setConfirmOpen(true)} disabled={apply.isPending}>
          Save changes
        </Button>
      </div>

      {isConflict ? (
        <Alert variant="destructive" data-testid="yaml-conflict">
          <RefreshCw className="h-4 w-4" />
          <AlertTitle>This object changed in the cluster</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>
              Someone (or a controller) updated this {kind} since you started editing. Saving now
              would overwrite their change, so it was rejected. Reload the latest version and
              re-apply your edit.
            </p>
            <Button variant="outline" size="sm" onClick={reloadLatest} disabled={yaml.isFetching}>
              <RefreshCw className={yaml.isFetching ? "animate-spin" : undefined} /> Reload latest
            </Button>
          </AlertDescription>
        </Alert>
      ) : (
        applyError && (
          <Alert variant="destructive" data-testid="yaml-validation-error">
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>Could not apply</AlertTitle>
            <AlertDescription>
              {applyError.message} ({applyError.code})
            </AlertDescription>
          </Alert>
        )
      )}

      <YamlEditor value={draft} onChange={setDraft} />

      <ConfirmDialog
        open={confirmOpen}
        title={`Apply changes to ${kind} ${refx.name}?`}
        description={`This updates the live object in the cluster${
          refx.namespace ? ` in namespace ${refx.namespace}` : ""
        }.`}
        confirmLabel="Apply"
        pending={apply.isPending}
        onConfirm={doApply}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
}

function YamlError({ error }: { error: Error }) {
  const detail = error instanceof ApiError ? `${error.message} (${error.code})` : error.message;
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>Failed to load YAML</AlertTitle>
      <AlertDescription>{detail}</AlertDescription>
    </Alert>
  );
}
