// Maps a resource route (URL-token group + plural resource) to its typed detail
// view (Sprint 7). The generic Summary view is the fallback for everything not
// registered here — including CRDs. Pure so it can be unit-tested and reused by
// the detail page's dispatch.

export type DetailKind =
  | "namespace"
  | "configmap"
  | "secret"
  | "service"
  | "ingress"
  | "role"
  | "clusterrole"
  | "rolebinding"
  | "clusterrolebinding"
  | "serviceaccount"
  | "persistentvolume"
  | "persistentvolumeclaim"
  | "storageclass";

/** Keyed by `${groupToken}/${resource}` — group is the URL token ("core" for the
 *  core group), resource the plural. Version-insensitive by construction. */
const REGISTRY: Record<string, DetailKind> = {
  "core/namespaces": "namespace",
  "core/configmaps": "configmap",
  "core/secrets": "secret",
  "core/services": "service",
  "core/serviceaccounts": "serviceaccount",
  "core/persistentvolumeclaims": "persistentvolumeclaim",
  "core/persistentvolumes": "persistentvolume",
  "networking.k8s.io/ingresses": "ingress",
  "rbac.authorization.k8s.io/roles": "role",
  "rbac.authorization.k8s.io/clusterroles": "clusterrole",
  "rbac.authorization.k8s.io/rolebindings": "rolebinding",
  "rbac.authorization.k8s.io/clusterrolebindings": "clusterrolebinding",
  "storage.k8s.io/storageclasses": "storageclass",
};

/** The typed detail kind for a resource route, or undefined for the generic
 *  Summary fallback. `group` is the URL token, `resource` the plural. */
export function detailKind(ref: { group: string; resource: string }): DetailKind | undefined {
  return REGISTRY[`${ref.group}/${ref.resource}`];
}

/** Whether a route is the core Secret kind — its YAML tab is forced read-only
 *  (editing would apply the redaction marker over real data). */
export function isSecret(ref: { group: string; resource: string }): boolean {
  return detailKind(ref) === "secret";
}
