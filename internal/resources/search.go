package resources

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Global search (Sprint 7, Story 7.5): a case-insensitive name match across
// every discovered listable type in the active context. It degrades gracefully
// — a type that fails to list becomes a warning, not a failed search — and the
// result set is bounded so a large cluster can't return an unbounded payload.

const (
	// defaultSearchLimit caps returned matches when the caller gives no limit.
	defaultSearchLimit = 50
	// maxSearchLimit is the hard cap regardless of the requested limit.
	maxSearchLimit = 100
	// perTypeListCap bounds each per-type list so one huge collection can't
	// dominate the search; only the first page is scanned (best-effort).
	perTypeListCap = 500
	// searchConcurrency bounds simultaneous per-type list calls.
	searchConcurrency = 8
)

// searchSkipResources are listable but pointless to name-search (opaque,
// high-volume names), so they are excluded to keep search fast and useful.
var searchSkipResources = map[string]bool{
	"events": true, // both core/v1 and events.k8s.io/v1 use resource name "events"
}

// SearchResult is one name match. Group is the raw API group ("" for core); the
// frontend tokenizes it to build the resource route.
type SearchResult struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Namespaced bool   `json:"namespaced"`
}

// SearchResponse is the bounded, best-effort result set. Truncated flags that
// more matches existed than the limit; Warnings names types that failed to list.
type SearchResponse struct {
	Query     string         `json:"query"`
	Results   []SearchResult `json:"results"`
	Truncated bool           `json:"truncated"`
	Warnings  []string       `json:"warnings,omitempty"`
}

// SearchHandler serves GET /api/v1/search?q=<term>&limit=<n>: name matches
// across the active context's discovered types, bounded and partial-tolerant.
func SearchHandler(cluster Cluster, disco *DiscoveryService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeError(w, logger, http.StatusBadRequest, "invalid_request", "a q query parameter is required")
			return
		}
		limit := parseLimit(r.URL.Query().Get("limit"))

		result, err := disco.Get(false)
		if err != nil {
			writeEngineError(w, logger, "discovering resources for search", err, execGuidanceFor(cluster))
			return
		}
		dyn, err := cluster.Dynamic()
		if err != nil {
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}

		matches, warnings := searchResources(r.Context(), dyn, searchableGVRs(result), query)
		bounded, truncated := boundResults(matches, limit)
		writeJSON(w, logger, http.StatusOK, SearchResponse{
			Query:     query,
			Results:   bounded,
			Truncated: truncated,
			Warnings:  warnings,
		})
	}
}

// searchableGVRs flattens discovery into the de-duplicated resource set to
// search, dropping the opaque high-volume kinds.
func searchableGVRs(result DiscoveryResult) []APIResourceInfo {
	seen := map[schema.GroupVersionResource]bool{}
	var out []APIResourceInfo
	for _, g := range result.Groups {
		for _, res := range g.Resources {
			if searchSkipResources[res.Resource] {
				continue
			}
			gvr := schema.GroupVersionResource{Group: res.Group, Version: res.Version, Resource: res.Resource}
			if seen[gvr] {
				continue
			}
			seen[gvr] = true
			out = append(out, res)
		}
	}
	return out
}

// searchResources lists each type concurrently (bounded) and collects
// name-substring matches; per-type failures become warnings so the search still
// returns the types that succeeded.
func searchResources(ctx context.Context, dyn dynamic.Interface, resources []APIResourceInfo, query string) ([]SearchResult, []string) {
	var (
		mu       sync.Mutex
		matches  []SearchResult
		warnings []string
		wg       sync.WaitGroup
		sem      = make(chan struct{}, searchConcurrency)
	)

	for _, info := range resources {
		wg.Add(1)
		go func(info APIResourceInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			gvr := schema.GroupVersionResource{Group: info.Group, Version: info.Version, Resource: info.Resource}
			list, err := dyn.Resource(gvr).List(ctx, metav1.ListOptions{Limit: perTypeListCap})
			if err != nil {
				mu.Lock()
				warnings = append(warnings, fmt.Sprintf("%s: %v", info.Resource, err))
				mu.Unlock()
				return
			}

			var found []SearchResult
			for i := range list.Items {
				item := &list.Items[i]
				if matchName(item.GetName(), query) {
					found = append(found, SearchResult{
						Group:      info.Group,
						Version:    info.Version,
						Resource:   info.Resource,
						Kind:       info.Kind,
						Namespace:  item.GetNamespace(),
						Name:       item.GetName(),
						Namespaced: info.Namespaced,
					})
				}
			}
			if len(found) > 0 {
				mu.Lock()
				matches = append(matches, found...)
				mu.Unlock()
			}
		}(info)
	}
	wg.Wait()

	sort.Strings(warnings)
	return matches, warnings
}

// matchName reports a case-insensitive substring match of query in name.
func matchName(name, query string) bool {
	return strings.Contains(strings.ToLower(name), strings.ToLower(query))
}

// boundResults sorts matches into a stable order and truncates them to limit,
// reporting whether truncation dropped any. Ordering: shorter names first (a
// closer match to the query), then by resource / namespace / name.
func boundResults(results []SearchResult, limit int) ([]SearchResult, bool) {
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if len(a.Name) != len(b.Name) {
			return len(a.Name) < len(b.Name)
		}
		if a.Resource != b.Resource {
			return a.Resource < b.Resource
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	if len(results) > limit {
		return results[:limit], true
	}
	return results, false
}

// parseLimit clamps the requested limit to (0, maxSearchLimit], defaulting when
// absent or unparseable.
func parseLimit(raw string) int {
	if raw == "" {
		return defaultSearchLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultSearchLimit
	}
	if n > maxSearchLimit {
		return maxSearchLimit
	}
	return n
}
