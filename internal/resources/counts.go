package resources

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// resourceLister is the minimal slice of dynamic.ResourceInterface countResource
// needs — a paged List. dynamic.NamespaceableResourceInterface satisfies it, and
// tests supply a fake.
type resourceLister interface {
	List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
}

// Per-resource-type counts for the sidebar (ADR-0009). Best-effort: every
// discovered listable type is counted cluster-wide via the dynamic client with
// a bounded fan-out; a type that errors is simply omitted and the response is
// marked partial, so the sidebar degrades to "no count" rather than failing.

const (
	countConcurrency = 8   // simultaneous list calls
	countPageLimit   = 200 // items per page when the server omits a remaining count
	countMaxPages    = 25  // pagination cap (≈5000 items) before returning a floor
)

// CountsResponse maps "group/version/resource" (raw group, "" for core) to a
// count. Partial is true when at least one type could not be counted.
type CountsResponse struct {
	Counts  map[string]int `json:"counts"`
	Partial bool           `json:"partial"`
}

// CountsHandler serves GET /api/v1/counts: a count per discovered resource type,
// keyed identically to the sidebar nav (group/version/resource).
func CountsHandler(svc *DiscoveryService, cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := svc.Get(false)
		if err != nil {
			writeEngineError(w, logger, "discovering resources", err, classifierFor(cluster))
			return
		}
		dyn, err := cluster.Dynamic()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		type target struct {
			key string
			gvr schema.GroupVersionResource
		}
		var targets []target
		for _, g := range result.Groups {
			for _, res := range g.Resources {
				targets = append(targets, target{
					key: res.Group + "/" + res.Version + "/" + res.Resource,
					gvr: schema.GroupVersionResource{Group: res.Group, Version: res.Version, Resource: res.Resource},
				})
			}
		}

		counts := make(map[string]int, len(targets))
		partial := false
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, countConcurrency)
		for _, t := range targets {
			wg.Add(1)
			sem <- struct{}{}
			go func(t target) {
				defer wg.Done()
				defer func() { <-sem }()
				n, ok, exact := countResource(r.Context(), dyn.Resource(t.gvr))
				mu.Lock()
				defer mu.Unlock()
				if ok {
					counts[t.key] = n
					// A count that hit the pagination cap is a floor, not exact —
					// record it but mark the response partial so the UI can't read
					// the floor as authoritative.
					if !exact {
						partial = true
					}
				} else {
					partial = true
				}
			}(t)
		}
		wg.Wait()

		writeJSON(w, logger, http.StatusOK, CountsResponse{Counts: counts, Partial: partial})
	}
}

// countResource counts a resource cluster-wide. It lists a small page and reads
// the server's RemainingItemCount when present (O(1) payload); otherwise it
// paginates up to countMaxPages and sums. Returns (count, ok, exact): a list
// error yields (0, false, false); an accurate count (via RemainingItemCount or a
// fully-walked list) is (n, true, true); hitting the pagination cap yields a
// floor (total, true, false) so the caller can mark it partial rather than
// treating the floor as authoritative.
func countResource(ctx context.Context, ri resourceLister) (count int, ok, exact bool) {
	total := 0
	cont := ""
	for page := 0; page < countMaxPages; page++ {
		list, err := ri.List(ctx, metav1.ListOptions{Limit: countPageLimit, Continue: cont})
		if err != nil {
			return 0, false, false
		}
		total += len(list.Items)
		if rc := list.GetRemainingItemCount(); rc != nil {
			return total + int(*rc), true, true
		}
		cont = list.GetContinue()
		if cont == "" {
			return total, true, true
		}
	}
	return total, true, false // hit the page cap — a floor, not an exact count
}
