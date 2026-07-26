package graph

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	// DefaultDepth reaches Deployment → ReplicaSet → Pod → (Service, ConfigMap,
	// Secret, ServiceAccount, PVC): the whole "what is this workload made of"
	// picture, and no more.
	DefaultDepth = 3
	// MaxDepth caps what a caller may ask for. Beyond this a namespace-scoped
	// walk stops being a diagram and starts being a namespace dump.
	MaxDepth = 6
	// maxNodes bounds the whole graph. Reaching it sets Partial and stops
	// admitting nodes — the frontend says so rather than rendering a plausible
	// but incomplete picture.
	maxNodes = 150
	// maxFanOut bounds one parent's children before they collapse into a single
	// counted node.
	maxFanOut = 24
	// clubFrom is the size at which a *run series* (a Job's pods, a CronJob's
	// Jobs) collapses, regardless of the fan-out cap: two near-identical runs
	// already add more noise than signal.
	clubFrom = 2
)

// neverScaled are kinds no HorizontalPodAutoscaler can target, so the graph
// skips the HPA index entirely for a neighbourhood made only of these.
var neverScaled = map[string]bool{
	kindPod: true, kindService: true, kindConfigMap: true, kindSecret: true,
	kindPVC: true, "PersistentVolume": true, kindServiceAccount: true,
	kindIngress: true, kindHPA: true, kindEndpoints: true, kindEndpointSlice: true,
}

// Build walks outwards from a focus object and returns the graph DTO.
//
// The walk is a breadth-first traversal, so every node is first reached at its
// minimal hop count and Depth is exact. A failure to read the focus object is
// fatal (the caller classifies it); a failure to read a *neighbour* degrades
// the graph to Partial with a note instead — one forbidden resource type should
// not blank the whole diagram.
func Build(ctx context.Context, dyn dynamic.Interface, resolver Resolver, opts Options) (*Response, error) {
	depth := opts.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}
	b := &builder{
		store:    newStore(dyn, opts.Namespace),
		resolver: resolver,
		ns:       opts.Namespace,
		depth:    depth,
		nodes:    map[string]*Node{},
		edges:    map[string]*Edge{},
		clubbed:  map[string]string{},
		seen:     map[string]bool{},
	}
	if depth > MaxDepth {
		b.depth = MaxDepth
		b.note(fmt.Sprintf("requested depth %d exceeds the maximum of %d; the graph was built at depth %d",
			depth, MaxDepth, MaxDepth))
	}

	focus, err := b.store.getStrict(ctx, opts.Focus, opts.Name)
	if err != nil {
		return nil, err
	}

	root, _ := b.addNode(opts.Focus, focusNamespace(opts), opts.Name, focus, 0)
	root.Focus = true
	b.queue = append(b.queue, queued{id: root.ID, obj: focus, info: opts.Focus, depth: 0})
	for len(b.queue) > 0 {
		cur := b.queue[0]
		b.queue = b.queue[1:]
		if cur.depth >= b.depth {
			continue
		}
		b.expand(ctx, cur)
	}
	b.buildGroups()

	return b.response(opts), nil
}

// focusNamespace is the request namespace for a namespaced focus and "" for a
// cluster-scoped one, so the node id and the detail deep-link agree.
func focusNamespace(opts Options) string {
	if opts.Focus.Namespaced {
		return opts.Namespace
	}
	return ""
}

// queued is one pending expansion.
type queued struct {
	id    string
	obj   *unstructured.Unstructured
	info  ResourceInfo
	depth int
}

// neighbor is one edge the current node produces, before the target is
// resolved into a graph node.
type neighbor struct {
	info  ResourceInfo
	name  string
	obj   *unstructured.Unstructured // pre-fetched when the walk already holds it
	rel   Relation
	label string
	// inbound flips the edge: the relation runs neighbour → current (a Service
	// routes *to* this pod; an owner owns this child).
	inbound bool
	// aggregate replaces items with one counted stand-in node.
	aggregate bool
	items     []unstructured.Unstructured
}

type builder struct {
	store    *store
	resolver Resolver
	ns       string
	depth    int

	nodes     map[string]*Node
	nodeOrder []string
	edges     map[string]*Edge
	edgeOrder []string
	groups    []Group
	queue     []queued

	// clubbed maps "<kind>/<name>" of an object folded into an aggregate to that
	// aggregate's node id, so a later expansion reaching the same object links to
	// the aggregate instead of materializing a duplicate node next to it.
	clubbed map[string]string

	// Namespace indexes, each built at most once (see links.go).
	svcLinks  *serviceLinks
	hpas      map[string][]string
	ingresses map[string][]string
	pods      map[string]*unstructured.Unstructured

	partial bool
	seen    map[string]bool
	notes   []string
}

// note records why the graph is not the whole truth and marks it partial.
func (b *builder) note(msg string) {
	b.partial = true
	if b.seen[msg] {
		return
	}
	b.seen[msg] = true
	b.notes = append(b.notes, msg)
}

// listNoted lists a resource type, turning a failure or a truncated page into a
// note rather than an error — a graph missing one relation type is far more
// useful than no graph.
func (b *builder) listNoted(ctx context.Context, info ResourceInfo) ([]unstructured.Unstructured, error) {
	items, truncated, err := b.store.list(ctx, info)
	if err != nil {
		b.note(fmt.Sprintf("could not list %s in %s: %v", info.Resource, b.ns, err))
		return nil, err
	}
	if truncated {
		b.note(fmt.Sprintf("%s has more than %d %s; only the first %d by name were considered",
			b.ns, listLimit, info.Resource, listLimit))
	}
	return items, nil
}

// nodeID is the stable synthetic key for one object. It is not the UID: a node
// may stand for an object a spec references but that does not exist.
func nodeID(info ResourceInfo, namespace, name string) string {
	group := info.Group
	if group == "" {
		group = "core"
	}
	return group + "/" + info.Kind + "/" + namespace + "/" + name
}

// addNode admits an object to the graph, or returns nil once the node cap is
// reached. A revisit keeps the smaller depth (a safeguard — breadth-first order
// already reaches every node at its minimum first).
func (b *builder) addNode(info ResourceInfo, namespace, name string, obj *unstructured.Unstructured, depth int) (*Node, bool) {
	id := nodeID(info, namespace, name)
	if existing, ok := b.nodes[id]; ok {
		if depth < existing.Depth {
			existing.Depth = depth
		}
		return existing, false
	}
	if len(b.nodes) >= maxNodes {
		b.note(fmt.Sprintf("the graph hit its %d-node cap; relations beyond it are not shown — narrow the depth or focus a smaller object", maxNodes))
		return nil, false
	}
	n := &Node{
		ID: id,
		Ref: Ref{
			Group: info.Group, Version: info.Version, Resource: info.Resource,
			Kind: info.Kind, Namespace: namespace, Name: name,
		},
		Depth:   depth,
		Status:  nodeStatus(obj),
		Missing: obj == nil,
	}
	if n.Missing {
		n.Status = "Missing"
	}
	b.nodes[id] = n
	b.nodeOrder = append(b.nodeOrder, id)
	return n, true
}

// addAggregate admits one counted stand-in for a clubbed set of siblings and
// records its members so later expansions link to it rather than re-creating
// them. Aggregates are terminal — the walk never expands one.
func (b *builder) addAggregate(parentID string, info ResourceInfo, items []unstructured.Unstructured, depth int) *Node {
	id := "aggregate/" + parentID + "/" + info.Kind
	if existing, ok := b.nodes[id]; ok {
		return existing
	}
	if len(b.nodes) >= maxNodes {
		b.note(fmt.Sprintf("the graph hit its %d-node cap; relations beyond it are not shown — narrow the depth or focus a smaller object", maxNodes))
		return nil
	}
	n := &Node{
		ID: id,
		Ref: Ref{
			Group: info.Group, Version: info.Version, Resource: info.Resource,
			Kind: info.Kind, Namespace: b.ns,
		},
		Depth:     depth,
		Aggregate: true,
		Count:     len(items),
		Status:    tallyStatuses(items),
	}
	b.nodes[id] = n
	b.nodeOrder = append(b.nodeOrder, id)
	for i := range items {
		b.clubbed[info.Kind+"/"+items[i].GetName()] = id
	}
	return n
}

// addEdge records a directed link. A pair reachable more than one way (a
// ConfigMap consumed through both a volume and envFrom) stays one edge whose
// label lists every mechanism.
func (b *builder) addEdge(source, target string, rel Relation, label string) {
	if source == "" || target == "" || source == target {
		return
	}
	id := source + "->" + target
	if existing, ok := b.edges[id]; ok {
		existing.Label = mergeLabel(existing.Label, label)
		return
	}
	e := &Edge{ID: id, Source: source, Target: target, Relation: rel, Label: label}
	b.edges[id] = e
	b.edgeOrder = append(b.edgeOrder, id)
}

// mergeLabel appends a mechanism to a comma-separated label, exactly once.
func mergeLabel(existing, add string) string {
	if add == "" {
		return existing
	}
	if existing == "" {
		return add
	}
	for _, part := range strings.Split(existing, ", ") {
		if part == add {
			return existing
		}
	}
	return existing + ", " + add
}

// expand resolves one node's neighbours into nodes and edges, enqueueing each
// newly-admitted object for its own expansion.
func (b *builder) expand(ctx context.Context, cur queued) {
	next := cur.depth + 1
	for _, nb := range b.neighbors(ctx, cur) {
		if nb.aggregate {
			agg := b.addAggregate(cur.id, nb.info, nb.items, next)
			if agg == nil {
				continue
			}
			b.link(cur.id, agg.ID, nb)
			continue
		}
		// An object already folded into an aggregate keeps that representation:
		// link to the aggregate rather than drawing it twice.
		if aggID, ok := b.clubbed[nb.info.Kind+"/"+nb.name]; ok {
			b.link(cur.id, aggID, nb)
			continue
		}
		obj := nb.obj
		if obj == nil {
			fetched, err := b.store.get(ctx, nb.info, nb.name)
			if err != nil {
				b.note(fmt.Sprintf("could not read %s %q: %v", nb.info.Kind, nb.name, err))
				continue
			}
			obj = fetched // nil ⇒ the reference dangles; addNode marks it Missing
		}
		ns := b.ns
		if !nb.info.Namespaced {
			ns = ""
		}
		node, isNew := b.addNode(nb.info, ns, nb.name, obj, next)
		if node == nil {
			continue
		}
		b.link(cur.id, node.ID, nb)
		if isNew && obj != nil {
			b.queue = append(b.queue, queued{id: node.ID, obj: obj, info: nb.info, depth: next})
		}
	}
}

// link draws the neighbour's edge in its true direction.
func (b *builder) link(curID, otherID string, nb neighbor) {
	if nb.inbound {
		b.addEdge(otherID, curID, nb.rel, nb.label)
		return
	}
	b.addEdge(curID, otherID, nb.rel, nb.label)
}

// neighbors derives every relation one object participates in. ownerReferences
// are walked for any kind (so a CRD's children come free); the kind-specific
// derivations below add the relations that live in specs rather than metadata.
func (b *builder) neighbors(ctx context.Context, cur queued) []neighbor {
	out := append(b.owners(cur), b.children(ctx, cur)...)

	switch cur.info.Kind {
	case kindPod:
		out = append(out, b.specRefs(podTemplateSpec(cur.obj, "spec"))...)
		out = append(out, b.servicesForPod(ctx, cur)...)
	case kindJob:
		// A Job's pods carry the same references, but the Job itself is the
		// object a reader recognizes — derive them from its pod template too.
		out = append(out, b.specRefs(podTemplateSpec(cur.obj, "spec", "template", "spec"))...)
	case kindService:
		out = append(out, b.podsForService(ctx, cur)...)
		out = append(out, b.ingressesForService(ctx, cur)...)
	case kindPVC:
		out = append(out, b.boundVolume(cur)...)
	case kindIngress:
		out = append(out, b.servicesForIngress(cur)...)
	case kindHPA:
		out = append(out, b.scaleTarget(cur)...)
	}
	out = append(out, b.autoscalers(ctx, cur)...)
	return out
}

// owners walks metadata.ownerReferences: the one relation every kind carries,
// which is why a CRD focus traverses without any per-kind knowledge.
func (b *builder) owners(cur queued) []neighbor {
	var out []neighbor
	for _, ref := range cur.obj.GetOwnerReferences() {
		info, ok := b.resolver.ByGroupKind(ref.APIVersion, ref.Kind)
		if !ok {
			continue
		}
		label := ""
		if ref.Controller != nil && *ref.Controller {
			label = "controller"
		}
		out = append(out, neighbor{info: info, name: ref.Name, rel: RelOwns, label: label, inbound: true})
	}
	return out
}

// children resolves the reverse of ownership. The API has no such index, so
// each candidate child kind is listed once and filtered by controller
// ownerReference UID — the resolution internal/resources/owned.go performs for
// the typed owned-pod lists, applied to unstructured objects here.
//
// This is also where clubbing happens: a run series (a Job's pods, a CronJob's
// Jobs) collapses from clubFrom upwards, and any fan-out past maxFanOut
// collapses whatever its kind, both with the count carried on the node.
func (b *builder) children(ctx context.Context, cur queued) []neighbor {
	kinds := ownerChildren[groupKind(cur.info.Group, cur.info.Kind)]
	if len(kinds) == 0 {
		return nil
	}
	uid := cur.obj.GetUID()
	var out []neighbor
	for _, kind := range kinds {
		info, ok := b.resolver.ByKind(kind)
		if !ok {
			continue
		}
		items, err := b.listNoted(ctx, info)
		if err != nil {
			continue
		}
		// Split already-drawn children from new ones: an object that reached the
		// graph another way (a Service's endpoint pod) keeps its own node, and
		// only the rest are candidates for clubbing, so nothing is counted twice.
		var existing, fresh []unstructured.Unstructured
		for i := range items {
			if !controlledBy(items[i].GetOwnerReferences(), uid) {
				continue
			}
			if _, drawn := b.nodes[nodeID(info, b.ns, items[i].GetName())]; drawn {
				existing = append(existing, items[i])
			} else {
				fresh = append(fresh, items[i])
			}
		}
		for i := range existing {
			out = append(out, neighbor{info: info, name: existing[i].GetName(), obj: &existing[i], rel: RelOwns})
		}
		if len(fresh) == 0 {
			continue
		}
		series := runSeries[groupKind(cur.info.Group, cur.info.Kind)+"|"+kind]
		overCap := len(fresh) > maxFanOut
		if overCap {
			b.note(fmt.Sprintf("%s %q owns %d %s — past the %d-per-node fan-out cap; they are summarized as one node",
				cur.info.Kind, cur.obj.GetName(), len(fresh), info.Resource, maxFanOut))
		}
		if overCap || (series && len(fresh) >= clubFrom) {
			label := ""
			if series {
				label = "runs"
			}
			out = append(out, neighbor{info: info, rel: RelOwns, label: label, aggregate: true, items: fresh})
			continue
		}
		for i := range fresh {
			out = append(out, neighbor{info: info, name: fresh[i].GetName(), obj: &fresh[i], rel: RelOwns})
		}
	}
	return out
}

// specRefs turns a pod spec's references (volumes, envFrom, env.valueFrom,
// imagePullSecrets, claims, serviceAccountName) into neighbours.
func (b *builder) specRefs(spec map[string]any) []neighbor {
	if spec == nil {
		return nil
	}
	var out []neighbor
	for _, ref := range podSpecRefs(spec) {
		info, ok := b.resolver.ByKind(ref.Kind)
		if !ok {
			continue
		}
		out = append(out, neighbor{info: info, name: ref.Name, rel: ref.Relation, label: ref.Label})
	}
	return out
}

// servicesForPod finds the Services fronting a pod (edge Service → Pod).
func (b *builder) servicesForPod(ctx context.Context, cur queued) []neighbor {
	info, ok := b.resolver.ByKind(kindService)
	if !ok {
		return nil
	}
	var out []neighbor
	for _, l := range b.serviceLinks(ctx).byPod[cur.obj.GetName()] {
		out = append(out, neighbor{info: info, name: l.Name, rel: RelRoutes, label: l.Label, inbound: true})
	}
	return out
}

// podsForService finds a Service's backing pods (edge Service → Pod), clubbing
// them once the fan-out cap is passed.
func (b *builder) podsForService(ctx context.Context, cur queued) []neighbor {
	info, ok := b.resolver.ByKind(kindPod)
	if !ok {
		return nil
	}
	links := b.serviceLinks(ctx).byService[cur.obj.GetName()]
	if len(links) == 0 {
		return nil
	}
	index := b.podIndex(ctx)
	var out []neighbor
	var items []unstructured.Unstructured
	missing := 0
	for _, l := range links {
		pod, ok := index[l.Name]
		if !ok {
			missing++ // an endpoint whose pod is gone, or past the list cap
			continue
		}
		items = append(items, *pod)
		out = append(out, neighbor{info: info, name: l.Name, obj: pod, rel: RelRoutes, label: l.Label})
	}
	if missing > 0 {
		b.note(fmt.Sprintf("service %q has %d endpoint(s) whose pod could not be read; they are not drawn",
			cur.obj.GetName(), missing))
	}
	if len(out) > maxFanOut {
		b.note(fmt.Sprintf("service %q fronts %d pods — past the %d-per-node fan-out cap; they are summarized as one node",
			cur.obj.GetName(), len(out), maxFanOut))
		return []neighbor{{info: info, rel: RelRoutes, aggregate: true, items: items}}
	}
	return out
}

// ingressesForService finds the Ingresses routing to a Service (edge
// Ingress → Service).
func (b *builder) ingressesForService(ctx context.Context, cur queued) []neighbor {
	info, ok := b.resolver.ByKind(kindIngress)
	if !ok {
		return nil
	}
	var out []neighbor
	for _, name := range b.ingressBackends(ctx)[cur.obj.GetName()] {
		out = append(out, neighbor{info: info, name: name, rel: RelRoutes, label: "backend", inbound: true})
	}
	return out
}

// servicesForIngress reads an Ingress's own backends (edge Ingress → Service).
func (b *builder) servicesForIngress(cur queued) []neighbor {
	info, ok := b.resolver.ByKind(kindService)
	if !ok {
		return nil
	}
	var out []neighbor
	for _, name := range ingressServices(cur.obj.Object) {
		out = append(out, neighbor{info: info, name: name, rel: RelRoutes, label: "backend"})
	}
	return out
}

// boundVolume follows a claim to its PersistentVolume — the one cluster-scoped
// hop a namespace-scoped graph makes, because the volume behind a claim is part
// of the workload's story.
func (b *builder) boundVolume(cur queued) []neighbor {
	name := nestedString(cur.obj.Object, "spec", "volumeName")
	if name == "" {
		return nil
	}
	info, ok := b.resolver.ByKind("PersistentVolume")
	if !ok {
		return nil
	}
	return []neighbor{{info: info, name: name, rel: RelClaims, label: "bound"}}
}

// scaleTarget follows an HPA's scaleTargetRef (edge HPA → target).
func (b *builder) scaleTarget(cur queued) []neighbor {
	apiVersion := nestedString(cur.obj.Object, "spec", "scaleTargetRef", "apiVersion")
	kind := nestedString(cur.obj.Object, "spec", "scaleTargetRef", "kind")
	name := nestedString(cur.obj.Object, "spec", "scaleTargetRef", "name")
	if kind == "" || name == "" {
		return nil
	}
	info, ok := b.resolver.ByGroupKind(apiVersion, kind)
	if !ok {
		return nil
	}
	return []neighbor{{info: info, name: name, rel: RelScales, label: "scaleTargetRef"}}
}

// autoscalers finds the HPAs targeting this object (edge HPA → target). The
// index is keyed by scaleTargetRef, so a CRD with a scale subresource is
// covered without naming it anywhere.
func (b *builder) autoscalers(ctx context.Context, cur queued) []neighbor {
	if neverScaled[cur.info.Kind] {
		return nil
	}
	info, ok := b.resolver.ByKind(kindHPA)
	if !ok {
		return nil
	}
	var out []neighbor
	for _, name := range b.hpaTargets(ctx)[cur.info.Kind+"/"+cur.obj.GetName()] {
		out = append(out, neighbor{info: info, name: name, rel: RelScales, label: "scaleTargetRef", inbound: true})
	}
	return out
}

// podIndex is the namespace's pods keyed by name, from the one cached list.
func (b *builder) podIndex(ctx context.Context) map[string]*unstructured.Unstructured {
	if b.pods != nil {
		return b.pods
	}
	index := map[string]*unstructured.Unstructured{}
	b.pods = index
	info, ok := b.resolver.ByKind(kindPod)
	if !ok {
		return index
	}
	items, err := b.listNoted(ctx, info)
	if err != nil {
		return index
	}
	for i := range items {
		index[items[i].GetName()] = &items[i]
	}
	return index
}

// controlledBy reports whether refs names uid as the controlling owner —
// the same test internal/resources/owned.go applies to typed objects.
func controlledBy(refs []metav1.OwnerReference, uid types.UID) bool {
	for i := range refs {
		if refs[i].UID == uid && refs[i].Controller != nil && *refs[i].Controller {
			return true
		}
	}
	return false
}

// response materializes the DTO in insertion order: the focus node first, then
// every node in the order the walk reached it — deterministic, because every
// list is name-sorted.
func (b *builder) response(opts Options) *Response {
	out := &Response{
		Namespace: b.ns,
		Focus: Ref{
			Group: opts.Focus.Group, Version: opts.Focus.Version, Resource: opts.Focus.Resource,
			Kind: opts.Focus.Kind, Namespace: focusNamespace(opts), Name: opts.Name,
		},
		Depth:   b.depth,
		Nodes:   make([]Node, 0, len(b.nodeOrder)),
		Edges:   make([]Edge, 0, len(b.edgeOrder)),
		Groups:  b.groups,
		Partial: b.partial,
		Notes:   b.notes,
	}
	if out.Groups == nil {
		out.Groups = []Group{}
	}
	for _, id := range b.nodeOrder {
		out.Nodes = append(out.Nodes, *b.nodes[id])
	}
	for _, id := range b.edgeOrder {
		out.Edges = append(out.Edges, *b.edges[id])
	}
	return out
}
