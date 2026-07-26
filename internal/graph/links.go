package graph

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Namespace-wide relation indexes. Each is built at most once per request from
// a single list call, then answers both directions — which pods a Service
// fronts *and* which Services front a pod — because a graph walk needs both and
// the API indexes neither.

// serviceEndpointLabel is the label the EndpointSlice controller stamps with the
// owning Service's name.
const serviceEndpointLabel = "kubernetes.io/service-name"

// link is one end of a Service⇄Pod pair, with how it was established.
type link struct {
	Name string
	// Label is "ready" / "not ready" for an endpoint-derived pair, or
	// "selector" for one matched from spec.selector because the Service has no
	// endpoints yet.
	Label string
}

// serviceLinks maps Services to their backing pods and back.
type serviceLinks struct {
	byService map[string][]link
	byPod     map[string][]link
}

func newServiceLinks() *serviceLinks {
	return &serviceLinks{byService: map[string][]link{}, byPod: map[string][]link{}}
}

func (l *serviceLinks) add(service, pod, label string) {
	if service == "" || pod == "" {
		return
	}
	for _, existing := range l.byService[service] {
		if existing.Name == pod {
			return // one pair, even if a pod appears in several slices/subsets
		}
	}
	l.byService[service] = append(l.byService[service], link{Name: pod, Label: label})
	l.byPod[pod] = append(l.byPod[pod], link{Name: service, Label: label})
}

func (l *serviceLinks) sortAll() {
	for _, m := range []map[string][]link{l.byService, l.byPod} {
		for k := range m {
			links := m[k]
			sort.Slice(links, func(i, j int) bool { return links[i].Name < links[j].Name })
		}
	}
}

// serviceLinks builds (once) the namespace's Service⇄Pod index.
//
// EndpointSlice is the source of truth on any cluster that serves it; the
// v1 Endpoints object is the fallback for older clusters (it is what
// internal/resources/services.go reads for the typed Service detail, so the two
// views agree). Either way only pod-backed addresses count: an endpoint with no
// pod behind it is a real backend but not a graph node in this namespace.
//
// A Service with no endpoints at all falls back to its spec.selector matched
// against the namespace's pods, so a Deployment whose pods are still starting
// still shows the Service that will front them — labelled "selector" so the
// distinction is visible rather than implied.
func (b *builder) serviceLinks(ctx context.Context) *serviceLinks {
	if b.svcLinks != nil {
		return b.svcLinks
	}
	links := newServiceLinks()
	b.svcLinks = links

	if info, ok := b.resolver.ByKind(kindEndpointSlice); ok {
		if slices, err := b.listNoted(ctx, info); err == nil {
			for i := range slices {
				service := slices[i].GetLabels()[serviceEndpointLabel]
				for _, e := range nestedSlice(slices[i].Object, "endpoints") {
					ep, ok := e.(map[string]any)
					if !ok {
						continue
					}
					if nestedString(ep, "targetRef", "kind") != kindPod {
						continue
					}
					// conditions.ready absent means ready, per the API contract.
					ready := true
					if v, found, err := unstructured.NestedBool(ep, "conditions", "ready"); found && err == nil {
						ready = v
					}
					links.add(service, nestedString(ep, "targetRef", "name"), readyLabel(ready))
				}
			}
		}
	}

	if len(links.byService) == 0 {
		if info, ok := b.resolver.ByKind(kindEndpoints); ok {
			if endpoints, err := b.listNoted(ctx, info); err == nil {
				for i := range endpoints {
					service := endpoints[i].GetName() // an Endpoints object is named for its Service
					for _, s := range nestedSlice(endpoints[i].Object, "subsets") {
						subset, ok := s.(map[string]any)
						if !ok {
							continue
						}
						for _, field := range []string{"addresses", "notReadyAddresses"} {
							for _, a := range nestedSlice(subset, field) {
								addr, ok := a.(map[string]any)
								if !ok || nestedString(addr, "targetRef", "kind") != kindPod {
									continue
								}
								links.add(service, nestedString(addr, "targetRef", "name"), readyLabel(field == "addresses"))
							}
						}
					}
				}
			}
		}
	}

	b.addSelectorLinks(ctx, links)
	links.sortAll()
	return links
}

// addSelectorLinks fills in Services that resolved to no endpoint-backed pod by
// matching their selector against the namespace's pods.
func (b *builder) addSelectorLinks(ctx context.Context, links *serviceLinks) {
	svcInfo, ok := b.resolver.ByKind(kindService)
	if !ok {
		return
	}
	services, err := b.listNoted(ctx, svcInfo)
	if err != nil {
		return
	}
	var pending []map[string]string
	var names []string
	for i := range services {
		name := services[i].GetName()
		if len(links.byService[name]) > 0 {
			continue
		}
		selector, found, err := unstructured.NestedStringMap(services[i].Object, "spec", "selector")
		if !found || err != nil || len(selector) == 0 {
			continue // headless/selector-less: nothing to infer
		}
		names = append(names, name)
		pending = append(pending, selector)
	}
	if len(pending) == 0 {
		return
	}
	podInfo, ok := b.resolver.ByKind(kindPod)
	if !ok {
		return
	}
	pods, err := b.listNoted(ctx, podInfo)
	if err != nil {
		return
	}
	for i := range pods {
		labels := pods[i].GetLabels()
		for j, selector := range pending {
			if matchesSelector(selector, labels) {
				links.add(names[j], pods[i].GetName(), "selector")
			}
		}
	}
}

// hpaTargets maps "<kind>/<name>" to the HorizontalPodAutoscalers scaling it.
// Indexing by the scaleTargetRef (rather than probing per node) means a CRD with
// a scale subresource is covered for the price of one list.
func (b *builder) hpaTargets(ctx context.Context) map[string][]string {
	if b.hpas != nil {
		return b.hpas
	}
	targets := map[string][]string{}
	b.hpas = targets
	info, ok := b.resolver.ByKind(kindHPA)
	if !ok {
		return targets
	}
	items, err := b.listNoted(ctx, info)
	if err != nil {
		return targets
	}
	for i := range items {
		kind := nestedString(items[i].Object, "spec", "scaleTargetRef", "kind")
		name := nestedString(items[i].Object, "spec", "scaleTargetRef", "name")
		if kind == "" || name == "" {
			continue
		}
		targets[kind+"/"+name] = append(targets[kind+"/"+name], items[i].GetName())
	}
	return targets
}

// ingressBackends maps a Service name to the Ingresses routing to it, from both
// the default backend and every rule path.
func (b *builder) ingressBackends(ctx context.Context) map[string][]string {
	if b.ingresses != nil {
		return b.ingresses
	}
	backends := map[string][]string{}
	b.ingresses = backends
	info, ok := b.resolver.ByKind(kindIngress)
	if !ok {
		return backends
	}
	items, err := b.listNoted(ctx, info)
	if err != nil {
		return backends
	}
	for i := range items {
		name := items[i].GetName()
		for _, svc := range ingressServices(items[i].Object) {
			if !contains(backends[svc], name) {
				backends[svc] = append(backends[svc], name)
			}
		}
	}
	return backends
}

// ingressServices lists the Service names an Ingress routes to.
func ingressServices(o map[string]any) []string {
	var out []string
	if n := nestedString(o, "spec", "defaultBackend", "service", "name"); n != "" {
		out = append(out, n)
	}
	for _, r := range nestedSlice(o, "spec", "rules") {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		for _, p := range nestedSlice(rule, "http", "paths") {
			path, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if n := nestedString(path, "backend", "service", "name"); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// matchesSelector reports whether labels satisfies every key of a Service's
// equality-only spec.selector (the only selector shape a Service has).
func matchesSelector(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func readyLabel(ready bool) string {
	if ready {
		return "ready"
	}
	return "not ready"
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
