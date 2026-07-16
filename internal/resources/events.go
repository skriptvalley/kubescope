package resources

import (
	"log/slog"
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

// Related events (Sprint 3, Story 3.4): events filtered to a single object by
// involvedObject, shaped newest-first for the shared events panel on workload
// detail views.

// EventSummary is one shaped event row.
type EventSummary struct {
	Type     string `json:"type"`   // Normal | Warning
	Reason   string `json:"reason"` // e.g. Scheduled, BackOff, Unhealthy
	Message  string `json:"message"`
	Count    int32  `json:"count"`
	LastSeen string `json:"lastSeen,omitempty"` // RFC3339; the most recent occurrence
}

// EventsHandler serves GET /api/v1/events?namespace=&kind=&name=: the events
// whose involvedObject matches the given kind + name (+ namespace), newest
// first. Filtering is done by the apiserver via a field selector; the handler
// only shapes and sorts. Missing kind or name is a 400.
func EventsHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		namespace := q.Get("namespace")
		kind := q.Get("kind")
		name := q.Get("name")
		if kind == "" || name == "" {
			writeError(w, logger, http.StatusBadRequest, "invalid_request",
				"both kind and name query parameters are required")
			return
		}

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		selector := fields.Set{
			"involvedObject.kind": kind,
			"involvedObject.name": name,
		}
		if namespace != "" {
			selector["involvedObject.namespace"] = namespace
		}
		list, err := clientset.CoreV1().Events(namespace).List(r.Context(), metav1.ListOptions{
			FieldSelector: selector.AsSelector().String(),
		})
		if err != nil {
			writeEngineError(w, logger, "listing events", err, classifierFor(cluster))
			return
		}

		writeJSON(w, logger, http.StatusOK, workloadList[EventSummary]{Items: shapeEvents(list.Items)})
	}
}

// EventsFeedHandler serves GET /api/v1/events/feed?namespace=&type=: the
// cluster-wide (or per-namespace) events feed backing the Story 4.4 events
// page, newest-first, optionally filtered to a single type (Normal|Warning).
// It is the initial-paint + polling-fallback complement to the live watch→SSE
// stream, which delivers the same EventFeedRow shape.
func EventsFeedHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		namespace := q.Get("namespace")
		typeFilter := q.Get("type")
		if typeFilter != "" && typeFilter != corev1.EventTypeNormal && typeFilter != corev1.EventTypeWarning {
			writeError(w, logger, http.StatusBadRequest, "invalid_request",
				"type must be Normal or Warning when set")
			return
		}

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		list, err := clientset.CoreV1().Events(namespace).List(r.Context(), metav1.ListOptions{})
		if err != nil {
			writeEngineError(w, logger, "listing events", err, classifierFor(cluster))
			return
		}

		writeJSON(w, logger, http.StatusOK, workloadList[EventFeedRow]{Items: shapeEventFeed(list.Items, typeFilter)})
	}
}

// shapeEventFeed shapes core events into feed rows newest-first, dropping rows
// whose type does not match a non-empty type filter.
func shapeEventFeed(events []corev1.Event, typeFilter string) []EventFeedRow {
	sorted := make([]corev1.Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		return eventLastSeen(&sorted[i]).Time.After(eventLastSeen(&sorted[j]).Time)
	})
	items := make([]EventFeedRow, 0, len(sorted))
	for i := range sorted {
		if typeFilter != "" && sorted[i].Type != typeFilter {
			continue
		}
		items = append(items, shapeEventFeedRow(&sorted[i]))
	}
	return items
}

// shapeEvents converts core events to shaped rows sorted newest-first by their
// last occurrence.
func shapeEvents(events []corev1.Event) []EventSummary {
	sorted := make([]corev1.Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		return eventLastSeen(&sorted[i]).Time.After(eventLastSeen(&sorted[j]).Time)
	})
	items := make([]EventSummary, 0, len(sorted))
	for i := range sorted {
		e := &sorted[i]
		items = append(items, EventSummary{
			Type:     e.Type,
			Reason:   e.Reason,
			Message:  e.Message,
			Count:    eventCount(e),
			LastSeen: formatTimestamp(eventLastSeen(e)),
		})
	}
	return items
}

// eventLastSeen resolves the most recent occurrence of an event, tolerating
// both the legacy core-event fields (lastTimestamp) and the newer series/event
// time fields, falling back to creation time.
func eventLastSeen(e *corev1.Event) metav1.Time {
	if e.Series != nil && !e.Series.LastObservedTime.IsZero() {
		return metav1.Time{Time: e.Series.LastObservedTime.Time}
	}
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp
	}
	if !e.EventTime.IsZero() {
		return metav1.Time{Time: e.EventTime.Time}
	}
	if !e.FirstTimestamp.IsZero() {
		return e.FirstTimestamp
	}
	return e.CreationTimestamp
}

// eventCount resolves how many times an event has fired, preferring the series
// count (new-style) over the legacy count, defaulting to 1.
func eventCount(e *corev1.Event) int32 {
	if e.Series != nil && e.Series.Count > 0 {
		return e.Series.Count
	}
	if e.Count > 0 {
		return e.Count
	}
	return 1
}
