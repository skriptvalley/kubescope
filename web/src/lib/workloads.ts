// Workload-kind helpers: decide when a generic (group/version/resource) route
// should render a typed workload view instead of the generic engine's default,
// and map a resource to its involvedObject Kind for the events panel. Pure so it
// is trivially testable.

/** The seven typed workload resources, keyed by (group, version, resource). */
interface WorkloadKind {
  group: string; // URL-token group ("core" for the empty group)
  version: string;
  resource: string;
  kind: string; // involvedObject Kind
  /** Controllers own pods (or, for CronJob, Jobs) and get a drill-down view. */
  controller: boolean;
}

const WORKLOAD_KINDS: WorkloadKind[] = [
  { group: "core", version: "v1", resource: "pods", kind: "Pod", controller: false },
  { group: "apps", version: "v1", resource: "deployments", kind: "Deployment", controller: true },
  { group: "apps", version: "v1", resource: "statefulsets", kind: "StatefulSet", controller: true },
  { group: "apps", version: "v1", resource: "daemonsets", kind: "DaemonSet", controller: true },
  { group: "apps", version: "v1", resource: "replicasets", kind: "ReplicaSet", controller: true },
  { group: "batch", version: "v1", resource: "jobs", kind: "Job", controller: true },
  { group: "batch", version: "v1", resource: "cronjobs", kind: "CronJob", controller: true },
];

export type WorkloadRef = { group: string; version: string; resource: string };

/** Resolves the typed workload kind for a route, or undefined for anything the
 *  generic engine should handle. */
export function workloadKind(ref: WorkloadRef): WorkloadKind | undefined {
  return WORKLOAD_KINDS.find(
    (k) => k.group === ref.group && k.version === ref.version && k.resource === ref.resource,
  );
}

/** Whether a route maps to a typed workload view at all. */
export function isWorkload(ref: WorkloadRef): boolean {
  return workloadKind(ref) !== undefined;
}

/** A client-side detail route for a controller/pod owner by Kind, or undefined
 *  for kinds Kubescope has no typed route for (e.g. Node owners). */
export function routeForKind(kind: string, namespace: string, name: string): string | undefined {
  const k = WORKLOAD_KINDS.find((w) => w.kind === kind);
  if (!k) return undefined;
  return `/resources/${k.group}/${k.version}/${k.resource}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`;
}
