import { AlertCircle } from "lucide-react";
import { useState } from "react";
import { useParams } from "react-router-dom";

import { ControllerDetail } from "@/components/controller-detail";
import { PodDetail } from "@/components/pod-detail";
import { YamlView } from "@/components/yaml-view";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useResourceObject, useResourceYaml } from "@/hooks/use-resource";
import { formatAge } from "@/lib/age";
import { ApiError, type KubeObject } from "@/lib/api";
import { cn } from "@/lib/utils";
import { workloadKind } from "@/lib/workloads";

type Tab = "summary" | "yaml";

export function ResourceDetailPage() {
  const params = useParams();
  const group = params.group ?? "";
  const version = params.version ?? "";
  const resource = params.resource ?? "";
  const namespace = params.namespace; // undefined for cluster-scoped objects
  const name = params.name ?? "";

  const ref = { group, version, resource, namespace, name };
  const [tab, setTab] = useState<Tab>("summary");
  const workload = workloadKind({ group, version, resource });
  // Controller detail resolves its own data and never renders the raw object, so
  // skip the object fetch for those kinds — the YAML tab uses a separate query.
  const object = useResourceObject(ref, !workload?.controller);
  const yaml = useResourceYaml(ref, tab === "yaml");

  const kind = workload?.kind ?? object.data?.kind ?? resource;

  return (
    <Card>
      <CardHeader className="space-y-1.5">
        <CardTitle className="break-all">{name}</CardTitle>
        <CardDescription>
          {kind} · {group === "core" ? "core" : group}/{version}
          {namespace ? ` · namespace ${namespace}` : ""}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="flex gap-1 border-b" role="tablist" aria-label="Object views">
          <TabButton active={tab === "summary"} onClick={() => setTab("summary")}>
            Summary
          </TabButton>
          <TabButton active={tab === "yaml"} onClick={() => setTab("yaml")}>
            YAML
          </TabButton>
        </div>

        {tab === "yaml" ? (
          yaml.isPending ? (
            <Skeleton className="h-64 w-full" data-testid="yaml-loading" />
          ) : yaml.isError ? (
            <DetailError error={yaml.error} />
          ) : (
            <YamlView yaml={yaml.data} />
          )
        ) : workload?.controller ? (
          // Controller detail resolves its own status/pods/events; the generic
          // object fetch is skipped here and only the YAML tab loads it lazily.
          <ControllerDetail resource={resource} kind={workload.kind} namespace={namespace ?? ""} name={name} />
        ) : object.isPending ? (
          <Skeleton className="h-40 w-full" data-testid="detail-loading" />
        ) : object.isError ? (
          <DetailError error={object.error} />
        ) : workload ? (
          <PodDetail object={object.data} namespace={namespace} name={name} />
        ) : (
          <Summary object={object.data} />
        )}
      </CardContent>
    </Card>
  );
}

function Summary({ object }: { object: KubeObject }) {
  const meta = object.metadata ?? {};
  const labels = Object.entries(meta.labels ?? {});
  const annotations = Object.entries(meta.annotations ?? {});
  const owners = meta.ownerReferences ?? [];

  return (
    <div className="space-y-6">
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Field label="Kind" value={object.kind ?? "—"} />
        <Field label="Namespace" value={meta.namespace ?? "—"} />
        <Field label="Age" value={formatAge(meta.creationTimestamp)} />
        <Field label="Created" value={meta.creationTimestamp ?? "—"} />
      </dl>

      <Panel title="Labels">
        {labels.length === 0 ? (
          <Empty />
        ) : (
          <div className="flex flex-wrap gap-2">
            {labels.map(([k, v]) => (
              <Badge key={k} variant="secondary" className="font-normal">
                {k}={v}
              </Badge>
            ))}
          </div>
        )}
      </Panel>

      <Panel title="Annotations">
        {annotations.length === 0 ? (
          <Empty />
        ) : (
          <dl className="space-y-1 text-sm">
            {annotations.map(([k, v]) => (
              <div key={k} className="grid grid-cols-1 gap-0.5 sm:grid-cols-[minmax(0,20rem)_1fr]">
                <dt className="truncate font-medium text-muted-foreground" title={k}>
                  {k}
                </dt>
                <dd className="break-all">{v}</dd>
              </div>
            ))}
          </dl>
        )}
      </Panel>

      <Panel title="Owner references">
        {owners.length === 0 ? (
          <Empty />
        ) : (
          <ul className="space-y-1 text-sm">
            {owners.map((o) => (
              <li key={o.uid ?? `${o.kind}/${o.name}`} className="flex items-center gap-2">
                <span className="font-medium">
                  {o.kind}/{o.name}
                </span>
                {o.controller && (
                  <Badge variant="outline" className="font-normal">
                    controller
                  </Badge>
                )}
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 break-all text-sm font-medium">{value}</dd>
    </div>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold">{title}</h3>
      {children}
    </section>
  );
}

function Empty() {
  return <p className="text-sm text-muted-foreground">None</p>;
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "-mb-px border-b-2 px-3 py-1.5 text-sm transition-colors",
        active
          ? "border-foreground font-medium text-foreground"
          : "border-transparent text-muted-foreground hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function DetailError({ error }: { error: Error }) {
  const apiError = error instanceof ApiError ? error : undefined;
  const title =
    apiError?.code === "not_found"
      ? "Not found"
      : apiError?.code === "forbidden"
        ? "Access denied"
        : "Failed to load object";
  const detail = apiError ? `${apiError.message} (${apiError.code})` : error.message;
  return (
    <Alert variant="destructive">
      <AlertCircle className="h-4 w-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{detail}</AlertDescription>
    </Alert>
  );
}
