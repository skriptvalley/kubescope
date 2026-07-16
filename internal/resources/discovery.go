package resources

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// coreGroupToken is the URL path segment standing in for the empty core API
// group, since "" cannot be a path segment. Generic routes use it and the
// engine maps it back to "".
const coreGroupToken = "core"

// DiscoveryResult is the sidebar-shaped enumeration of everything the active
// cluster serves: browsable resources grouped by API group, plus any
// per-group discovery failures surfaced as warnings (partial discovery still
// returns the groups that succeeded).
type DiscoveryResult struct {
	Groups   []APIGroupInfo `json:"groups"`
	Warnings []string       `json:"warnings,omitempty"`
}

// APIGroupInfo is one API group and its browsable resources. Name is "" for
// the core group.
type APIGroupInfo struct {
	Name      string            `json:"name"`
	Resources []APIResourceInfo `json:"resources"`
}

// APIResourceInfo carries everything the UI and the generic get/list handlers
// need for one resource type: its GVR, kind, scope and supported verbs.
type APIResourceInfo struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
}

// DiscoveryCluster is the slice of Cluster the discovery service needs. The
// full *kube.Manager satisfies it; tests supply a fake. DiscoveryFor takes an
// explicit context name so the service can resolve the active context once and
// fetch + cache under that same name — a concurrent switch cannot then store
// one cluster's resources under another's cache key.
type DiscoveryCluster interface {
	ActiveContextName() (string, error)
	DiscoveryFor(name string) (discovery.DiscoveryInterface, error)
	SourceGeneration() int64
}

// discoveryKey identifies one cache entry: the context name plus the
// kubeconfig-source generation it was fetched under (ADR-0007).
type discoveryKey struct {
	name string
	gen  int64
}

// DiscoveryService enumerates API groups/resources per context and caches the
// result. The cache is keyed by context name + source generation, so switching
// contexts — or swapping the kubeconfig source at runtime — uses a separate
// entry and never returns another cluster's resources. An explicit refresh
// re-fetches, so a CRD installed after startup appears without a restart
// (ADR-0003).
type DiscoveryService struct {
	cluster DiscoveryCluster

	mu    sync.RWMutex
	cache map[discoveryKey]DiscoveryResult
}

// NewDiscoveryService returns a discovery service backed by the given cluster.
func NewDiscoveryService(cluster DiscoveryCluster) *DiscoveryService {
	return &DiscoveryService{cluster: cluster, cache: make(map[discoveryKey]DiscoveryResult)}
}

// Get returns the discovery result for the active context, serving a cached
// copy unless refresh is true. Kubeconfig problems surface as *kubeconfigError
// (→ 503); an unreachable cluster surfaces as a raw error (→ 502).
func (s *DiscoveryService) Get(refresh bool) (DiscoveryResult, error) {
	name, err := s.cluster.ActiveContextName()
	if err != nil {
		return DiscoveryResult{}, &kubeconfigError{err}
	}
	// The key carries the source generation: after a runtime kubeconfig swap
	// (ADR-0007) a same-named context must not serve the old file's cached
	// catalog. Old-generation entries are left behind (bounded by swap count).
	key := discoveryKey{name: name, gen: s.cluster.SourceGeneration()}

	if !refresh {
		s.mu.RLock()
		cached, ok := s.cache[key]
		s.mu.RUnlock()
		if ok {
			return cached, nil
		}
	}

	// Fetch discovery for the exact name we cache under, not "whatever is active
	// now" — otherwise a switch between here and the cache write could store the
	// new cluster's resources under the old context's key.
	disc, err := s.cluster.DiscoveryFor(name)
	if err != nil {
		return DiscoveryResult{}, &kubeconfigError{err}
	}
	lists, derr := disc.ServerPreferredResources()
	result, err := shapeDiscovery(lists, derr)
	if err != nil {
		return DiscoveryResult{}, err
	}

	s.mu.Lock()
	s.cache[key] = result
	s.mu.Unlock()
	return result, nil
}

// Resolve looks up a GVR in the (cached) discovery data, reporting whether the
// active cluster serves it and its scope. A cluster-side discovery failure is
// returned as an error; an unknown-but-reachable GVR is (_, false, nil).
func (s *DiscoveryService) Resolve(gvr schema.GroupVersionResource) (APIResourceInfo, bool, error) {
	result, err := s.Get(false)
	if err != nil {
		return APIResourceInfo{}, false, err
	}
	for _, g := range result.Groups {
		for _, r := range g.Resources {
			if r.Group == gvr.Group && r.Version == gvr.Version && r.Resource == gvr.Resource {
				return r, true, nil
			}
		}
	}
	return APIResourceInfo{}, false, nil
}

// shapeDiscovery turns the apiserver's preferred-resource lists into the
// sidebar-shaped result: one entry per browsable (listable, non-subresource)
// resource, grouped and sorted, with per-group failures degraded to warnings.
func shapeDiscovery(lists []*metav1.APIResourceList, err error) (DiscoveryResult, error) {
	var warnings []string
	if err != nil {
		var gdf *discovery.ErrGroupDiscoveryFailed
		if errors.As(err, &gdf) {
			for gv, gerr := range gdf.Groups {
				warnings = append(warnings, gv.String()+": "+gerr.Error())
			}
			sort.Strings(warnings)
		} else {
			// A total discovery failure (e.g. unreachable apiserver) is not a
			// partial degradation — surface it so the handler returns a 502.
			return DiscoveryResult{}, err
		}
	}

	byGroup := map[string][]APIResourceInfo{}
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, perr := schema.ParseGroupVersion(list.GroupVersion)
		if perr != nil {
			continue
		}
		for _, res := range list.APIResources {
			if strings.Contains(res.Name, "/") { // subresource (e.g. pods/status)
				continue
			}
			if !containsVerb(res.Verbs, "list") { // not browsable as a collection
				continue
			}
			byGroup[gv.Group] = append(byGroup[gv.Group], APIResourceInfo{
				Group:      gv.Group,
				Version:    gv.Version,
				Resource:   res.Name,
				Kind:       res.Kind,
				Namespaced: res.Namespaced,
				Verbs:      res.Verbs,
			})
		}
	}

	groups := make([]APIGroupInfo, 0, len(byGroup))
	for name, resources := range byGroup {
		sort.Slice(resources, func(i, j int) bool { return resources[i].Resource < resources[j].Resource })
		groups = append(groups, APIGroupInfo{Name: name, Resources: resources})
	}
	// Core group first, then alphabetical — a stable, predictable sidebar order.
	sort.Slice(groups, func(i, j int) bool {
		if (groups[i].Name == "") != (groups[j].Name == "") {
			return groups[i].Name == ""
		}
		return groups[i].Name < groups[j].Name
	})

	return DiscoveryResult{Groups: groups, Warnings: warnings}, nil
}

func containsVerb(verbs []string, want string) bool {
	for _, v := range verbs {
		if v == want {
			return true
		}
	}
	return false
}

// DiscoveryHandler serves GET /api/v1/discovery: the sidebar-shaped resource
// enumeration for the active context. `?refresh=true` bypasses the cache so a
// newly-installed CRD appears without a restart.
func DiscoveryHandler(svc *DiscoveryService, cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		refresh := r.URL.Query().Get("refresh") == "true"
		result, err := svc.Get(refresh)
		if err != nil {
			writeEngineError(w, logger, "discovering resources", err, classifierFor(cluster))
			return
		}
		writeJSON(w, logger, http.StatusOK, result)
	}
}
