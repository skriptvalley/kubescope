package resources

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/skriptvalley/kubescope/internal/graph"
)

// Resource relationship graph (FB-14, ADR-0011). The traversal itself lives in
// internal/graph; this file is the HTTP edge: parse and validate the focus,
// resolve it against the shared discovery cache, and map failures onto the same
// classified error envelope every other read uses.

// GraphHandler serves GET
// /api/v1/namespaces/{namespace}/graph?focus=<kind>/<name>&depth=<N>: the
// bounded relationship graph around one object. A read — not gated by read-only
// mode.
func GraphHandler(svc *DiscoveryService, cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := chi.URLParam(r, "namespace")

		kind, name, ok := strings.Cut(r.URL.Query().Get("focus"), "/")
		if !ok || kind == "" || name == "" {
			writeError(w, logger, http.StatusBadRequest, "invalid_focus",
				`focus must be "<kind>/<name>", e.g. focus=Deployment/frontend`)
			return
		}

		depth, err := parseDepth(r.URL.Query().Get("depth"))
		if err != nil {
			writeError(w, logger, http.StatusBadRequest, "invalid_depth", err.Error())
			return
		}

		result, err := svc.Get(false)
		if err != nil {
			writeEngineError(w, logger, "discovering resources", err, classifierFor(cluster))
			return
		}
		resolver := newGraphResolver(result)
		focus, found := resolver.ByKind(kind)
		if !found {
			writeError(w, logger, http.StatusNotFound, "unknown_resource",
				fmt.Sprintf("the active cluster serves no kind %q", kind))
			return
		}
		if !focus.Namespaced {
			writeInvalidScope(w, logger,
				"%s is cluster-scoped and cannot be the focus of a namespace graph", focus.Kind)
			return
		}

		dyn, err := cluster.Dynamic()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		response, err := graph.Build(r.Context(), dyn, resolver, graph.Options{
			Namespace: namespace,
			Focus:     focus,
			Name:      name,
			Depth:     depth,
		})
		if err != nil {
			writeEngineError(w, logger,
				fmt.Sprintf("building the resource graph for %s %q", focus.Kind, name),
				err, classifierFor(cluster))
			return
		}
		writeJSON(w, logger, http.StatusOK, response)
	}
}

// parseDepth reads the depth parameter. Absent means the builder's default; a
// value above the maximum is clamped there (and reported in the response's
// notes), but a non-numeric or negative one is a client mistake worth naming.
func parseDepth(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	depth, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("depth must be a whole number between 1 and %d", graph.MaxDepth)
	}
	if depth < 1 {
		return 0, fmt.Errorf("depth must be at least 1 (got %d)", depth)
	}
	return depth, nil
}

// graphResolver indexes one discovery snapshot so the builder resolves kinds
// without re-reading the cache per hop. Taking a snapshot also means a graph is
// built against a single, self-consistent view of what the cluster serves.
type graphResolver struct {
	byKind      map[string]graph.ResourceInfo
	byResource  map[string]graph.ResourceInfo
	byGroupKind map[string]graph.ResourceInfo
}

func newGraphResolver(result DiscoveryResult) *graphResolver {
	r := &graphResolver{
		byKind:      map[string]graph.ResourceInfo{},
		byResource:  map[string]graph.ResourceInfo{},
		byGroupKind: map[string]graph.ResourceInfo{},
	}
	for _, g := range result.Groups {
		for _, res := range g.Resources {
			info := graph.ResourceInfo{
				Group: res.Group, Version: res.Version, Resource: res.Resource,
				Kind: res.Kind, Namespaced: res.Namespaced,
			}
			r.byGroupKind[res.Group+"/"+res.Kind] = info
			// A Kind can be served by more than one group (an operator shipping its
			// own "Ingress", say). Prefer the built-in group so an unqualified focus
			// resolves to what the user almost certainly means, deterministically.
			r.putPreferred(r.byKind, strings.ToLower(res.Kind), info)
			r.putPreferred(r.byResource, strings.ToLower(res.Resource), info)
		}
	}
	return r
}

func (r *graphResolver) putPreferred(index map[string]graph.ResourceInfo, key string, info graph.ResourceInfo) {
	existing, ok := index[key]
	if !ok || betterGroup(info.Group, existing.Group) {
		index[key] = info
	}
}

// betterGroup ranks the built-in groups ahead of everything else, then falls
// back to alphabetical order so the choice never depends on map iteration.
func betterGroup(candidate, current string) bool {
	c, cur := groupRank(candidate), groupRank(current)
	if c != cur {
		return c < cur
	}
	return candidate < current
}

func groupRank(group string) int {
	switch group {
	case "":
		return 0
	case "apps":
		return 1
	case "batch":
		return 2
	case "networking.k8s.io":
		return 3
	case "autoscaling":
		return 4
	case "discovery.k8s.io":
		return 5
	default:
		return 10
	}
}

// ByKind resolves the focus parameter and every kind the builder names
// internally. It accepts a Kind ("Deployment"), a plural resource
// ("deployments") or the singular form ("deployment", which is the lowercased
// Kind) — all case-insensitively, because the focus arrives from a URL.
func (r *graphResolver) ByKind(kind string) (graph.ResourceInfo, bool) {
	key := strings.ToLower(kind)
	if info, ok := r.byKind[key]; ok {
		return info, true
	}
	info, ok := r.byResource[key]
	return info, ok
}

// ByGroupKind resolves an apiVersion + Kind pair as ownerReferences and
// scaleTargetRefs carry it, falling back to the group-less lookup when the
// reference omits its apiVersion.
func (r *graphResolver) ByGroupKind(apiVersion, kind string) (graph.ResourceInfo, bool) {
	if apiVersion == "" {
		return r.ByKind(kind)
	}
	// "apps/v1" → "apps"; a bare "v1" is the core group.
	group, _, _ := strings.Cut(apiVersion, "/")
	if !strings.Contains(apiVersion, "/") {
		group = ""
	}
	if info, ok := r.byGroupKind[group+"/"+kind]; ok {
		return info, true
	}
	return r.ByKind(kind)
}
