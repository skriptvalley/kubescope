// Package graph assembles a bounded, namespace-scoped resource relationship
// graph around a focus object: the typed {nodes, edges, groups} DTO the
// frontend renders as a compound-node topology (ADR-0011).
//
// Every read goes through the dynamic client against GVRs resolved from
// discovery (ADR-0003), so a CRD focus — or a custom controller that owns core
// objects through ownerReferences — traverses exactly like a built-in kind.
//
// The graph is deliberately bounded on three axes: one namespace, a focus
// object, and a small depth. Where a bound bites, the result says so via
// Partial + a note; nothing is ever dropped silently.
package graph

// Relation names what an edge expresses. The frontend styles edges by relation,
// so these strings are part of the API contract.
type Relation string

const (
	// RelOwns is an ownerReference link, drawn owner → child
	// (Deployment→ReplicaSet→Pod, CronJob→Job, …).
	RelOwns Relation = "owns"
	// RelRoutes is traffic flow: Service → Pod, Ingress → Service.
	RelRoutes Relation = "routes"
	// RelMounts is a volume-mounted ConfigMap/Secret.
	RelMounts Relation = "mounts"
	// RelEnv is a ConfigMap/Secret consumed through envFrom or env.valueFrom.
	RelEnv Relation = "env"
	// RelImagePullSecret is a Secret referenced by spec.imagePullSecrets.
	RelImagePullSecret Relation = "imagePullSecret"
	// RelClaims is Pod → PersistentVolumeClaim → PersistentVolume.
	RelClaims Relation = "claims"
	// RelServiceAccount is Pod → ServiceAccount.
	RelServiceAccount Relation = "serviceAccount"
	// RelScales is HorizontalPodAutoscaler → its scaleTargetRef.
	RelScales Relation = "scales"
)

// Ref identifies one object well enough for the frontend to deep-link to its
// detail route: the raw API group ("" for core, tokenized client-side), the
// version and plural resource, plus kind/namespace/name.
type Ref struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// Node is one vertex. ID is a stable synthetic key (group/kind/namespace/name)
// — not a UID, because a node may stand for an object that a spec references
// but that does not exist (Missing), or for a clubbed set of objects
// (Aggregate).
type Node struct {
	ID string `json:"id"`
	Ref
	// Status is a compact, kind-appropriate status string ("Running", "2/3",
	// "Bound", …) or "" for kinds that have none. Tone is derived client-side by
	// the centralized status→tone classifier (ADR-0009), not here.
	Status string `json:"status,omitempty"`
	// Depth is the number of hops from the focus object (focus itself is 0).
	Depth int `json:"depth"`
	// Focus marks the object the graph was built around.
	Focus bool `json:"focus,omitempty"`
	// Parent is the id of the compound group this node renders inside, if any.
	Parent string `json:"parent,omitempty"`
	// Aggregate marks a clubbed stand-in for Count sibling objects of the same
	// kind (a run series, or a fan-out past the cap). Aggregates are terminal:
	// the builder does not expand them.
	Aggregate bool `json:"aggregate,omitempty"`
	// Count is the number of objects an Aggregate node stands for.
	Count int `json:"count,omitempty"`
	// Missing marks a referenced object that does not exist in the cluster — a
	// dangling configMap/secret/claim reference, which is worth seeing.
	Missing bool `json:"missing,omitempty"`
}

// Edge is one directed link between two node ids.
type Edge struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Relation Relation `json:"relation"`
	// Label names the mechanism behind the edge ("volume, envFrom", "ready",
	// "selector"). Merged when one pair is reachable more than one way.
	Label string `json:"label,omitempty"`
}

// Group is a compound parent: the box a workload's own nodes render inside
// (the Deployment circle containing its ReplicaSet, pods and Service). Nodes
// point at it through Node.Parent.
type Group struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Kind is the kind of the controller the group was built around.
	Kind string `json:"kind"`
	// Root is the node id of that controller.
	Root string `json:"root"`
}

// Response is the graph DTO.
type Response struct {
	Namespace string  `json:"namespace"`
	Focus     Ref     `json:"focus"`
	Depth     int     `json:"depth"`
	Nodes     []Node  `json:"nodes"`
	Edges     []Edge  `json:"edges"`
	Groups    []Group `json:"groups"`
	// Partial is true when a bound bit: a node cap, a fan-out cap, a truncated
	// list, a clamped depth or a neighbor lookup that failed. Notes says which.
	Partial bool     `json:"partial"`
	Notes   []string `json:"notes,omitempty"`
}

// ResourceInfo is one resource type's GVR and scope, as discovery reports it.
type ResourceInfo struct {
	Group      string
	Version    string
	Resource   string
	Kind       string
	Namespaced bool
}

// Resolver maps Kubernetes identities onto the GVRs the active cluster serves.
// The handler backs it with the shared discovery cache, so the graph sees
// exactly the types the sidebar does — CRDs included (ADR-0003).
type Resolver interface {
	// ByKind resolves a bare Kind (or a plural/singular resource name) with no
	// group context — the shape the focus query parameter arrives in.
	ByKind(kind string) (ResourceInfo, bool)
	// ByGroupKind resolves an apiVersion + Kind pair, as ownerReferences and
	// scaleTargetRefs carry them. An empty apiVersion falls back to ByKind.
	ByGroupKind(apiVersion, kind string) (ResourceInfo, bool)
}

// Options parameterizes one Build.
type Options struct {
	// Namespace scopes the whole traversal; cluster-scoped neighbours (a
	// PersistentVolume behind a claim) are the only objects outside it.
	Namespace string
	// Focus is the resolved type of the focus object, and Name its name.
	Focus ResourceInfo
	Name  string
	// Depth caps hops from the focus. Zero means DefaultDepth; anything above
	// MaxDepth is clamped (and noted).
	Depth int
}

// Well-known kinds the builder reasons about by name. Everything else still
// traverses generically through ownerReferences.
const (
	kindPod            = "Pod"
	kindService        = "Service"
	kindConfigMap      = "ConfigMap"
	kindSecret         = "Secret"
	kindPVC            = "PersistentVolumeClaim"
	kindServiceAccount = "ServiceAccount"
	kindEndpoints      = "Endpoints"
	kindEndpointSlice  = "EndpointSlice"
	kindIngress        = "Ingress"
	kindHPA            = "HorizontalPodAutoscaler"
	kindJob            = "Job"
	kindCronJob        = "CronJob"
	kindDeployment     = "Deployment"
	kindReplicaSet     = "ReplicaSet"
	kindStatefulSet    = "StatefulSet"
	kindDaemonSet      = "DaemonSet"
)

// groupKind is the "<group>/<kind>" key the relation tables are keyed by; the
// core group is the empty string, so a Pod is "/Pod".
func groupKind(group, kind string) string { return group + "/" + kind }

// ownerChildren lists, per controller kind, the child kinds whose
// ownerReferences may point back at it. Reverse ownership has no server-side
// index — finding a controller's children means listing candidate kinds and
// filtering by ownerReference UID, the same resolution internal/resources/
// owned.go performs for the typed owned-pod lists — so the search is restricted
// to the pairs that actually occur rather than sweeping the namespace.
var ownerChildren = map[string][]string{
	groupKind("apps", kindDeployment):  {kindReplicaSet},
	groupKind("apps", kindReplicaSet):  {kindPod},
	groupKind("apps", kindStatefulSet): {kindPod},
	groupKind("apps", kindDaemonSet):   {kindPod},
	groupKind("batch", kindJob):        {kindPod},
	groupKind("batch", kindCronJob):    {kindJob},
}

// runSeries marks the owner→child pairs that are a *series of runs* rather than
// a replica set of peers: a Job's pods (attempts) and a CronJob's Jobs
// (scheduled runs). Those club into a single aggregated node so a per-minute
// CronJob does not bury the graph in near-identical nodes (Story 2.2).
var runSeries = map[string]bool{
	groupKind("batch", kindJob) + "|" + kindPod:     true,
	groupKind("batch", kindCronJob) + "|" + kindJob: true,
}

// podControllers are the kinds whose graph neighbourhood (ReplicaSets, pods and
// the Services fronting them) is worth boxing into one compound group.
var podControllers = map[string]bool{
	groupKind("apps", kindDeployment):  true,
	groupKind("apps", kindStatefulSet): true,
	groupKind("apps", kindDaemonSet):   true,
	groupKind("apps", kindReplicaSet):  true,
	groupKind("batch", kindJob):        true,
	groupKind("batch", kindCronJob):    true,
}
