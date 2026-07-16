package resources

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Stream row shaping (Sprint 4): the watch→SSE bridge in internal/stream feeds
// live objects to the same list/detail views the REST handlers serve, so an
// event must carry exactly the row shape that view already renders — a typed
// summary for the seven workload kinds, an event-feed row for core Events, and
// the generic metadata row for everything else. Reusing the Sprint 3 shapers
// keeps the frontend thin: it patches its cache with server-shaped rows and
// never re-derives status/rollout logic client-side (ADR-0003, ADR-0006).

// Well-known GVRs the stream bridge shapes with typed summaries.
var (
	gvrPods         = schema.GroupVersionResource{Group: "", Version: "v1", Resource: resourcePods}
	gvrEvents       = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"}
	gvrDeployments  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resourceDeployments}
	gvrStatefulSets = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resourceStatefulSets}
	gvrDaemonSets   = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resourceDaemonSets}
	gvrReplicaSets  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resourceReplicaSets}
	gvrJobs         = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: resourceJobs}
	gvrCronJobs     = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: resourceCronJobs}
)

// InvolvedObjectRef points an event-feed row at the object the event is about,
// so the UI can deep-link to that object's detail view when it exists.
type InvolvedObjectRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// EventFeedRow is one shaped row for the cluster-wide/per-namespace events feed
// (Story 4.4). Name/Namespace identify the Event object itself (the stream keys
// rows by them); InvolvedObject is the resource the event concerns.
//
// UID and CreationTimestamp make the row a superset of the generic list row, so
// when core/v1/events is browsed through the generic engine (which paints from
// the listRow shape) the live-streamed rows still render an age and key exactly
// like the REST list's rows.
type EventFeedRow struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Type              string            `json:"type"` // Normal | Warning
	Reason            string            `json:"reason"`
	Message           string            `json:"message"`
	Count             int32             `json:"count"`
	LastSeen          string            `json:"lastSeen,omitempty"`
	InvolvedObject    InvolvedObjectRef `json:"involvedObject"`
}

// ShapeStreamRow shapes a live object from the watch bridge into the row its
// list/feed view expects: a typed workload summary for the seven workload
// kinds, an event-feed row for core Events, else the generic metadata row. The
// object arrives unstructured (dynamic informer); known kinds are converted to
// their typed form and run through the existing Sprint 3 shapers so the wire
// shape is identical to the REST list endpoints.
func ShapeStreamRow(gvr schema.GroupVersionResource, u *unstructured.Unstructured) any {
	switch gvr {
	case gvrPods:
		var pod corev1.Pod
		if fromUnstructured(u, &pod) {
			return shapePod(&pod)
		}
	case gvrDeployments:
		var d appsv1.Deployment
		if fromUnstructured(u, &d) {
			return shapeDeployment(&d)
		}
	case gvrStatefulSets:
		var s appsv1.StatefulSet
		if fromUnstructured(u, &s) {
			return shapeStatefulSet(&s)
		}
	case gvrDaemonSets:
		var d appsv1.DaemonSet
		if fromUnstructured(u, &d) {
			return shapeDaemonSet(&d)
		}
	case gvrReplicaSets:
		var rs appsv1.ReplicaSet
		if fromUnstructured(u, &rs) {
			return shapeReplicaSet(&rs)
		}
	case gvrJobs:
		var j batchv1.Job
		if fromUnstructured(u, &j) {
			return shapeJob(&j)
		}
	case gvrCronJobs:
		var c batchv1.CronJob
		if fromUnstructured(u, &c) {
			return shapeCronJob(&c)
		}
	case gvrEvents:
		var e corev1.Event
		if fromUnstructured(u, &e) {
			return shapeEventFeedRow(&e)
		}
	}
	return genericStreamRow(gvr, u)
}

// IsWorkloadStreamGVR reports whether a GVR is one of the seven typed workload
// kinds — callers key their cache the same way the typed list endpoints do.
func IsWorkloadStreamGVR(gvr schema.GroupVersionResource) bool {
	switch gvr {
	case gvrPods, gvrDeployments, gvrStatefulSets, gvrDaemonSets, gvrReplicaSets, gvrJobs, gvrCronJobs:
		return true
	default:
		return false
	}
}

// genericStreamRow is the same metadata row shapeList emits, built from an
// unstructured object so any GVR (incl. CRDs) can stream into a generic list.
// It applies the same per-kind column enrichment as the REST list so a live
// update carries the identical row shape the initial list painted (Sprint 7).
func genericStreamRow(gvr schema.GroupVersionResource, u *unstructured.Unstructured) listRow {
	row := listRow{
		Name:      u.GetName(),
		Namespace: u.GetNamespace(),
		UID:       string(u.GetUID()),
		Cells:     enrichRow(enrichmentFor(gvr), u),
	}
	if ts := u.GetCreationTimestamp(); !ts.IsZero() {
		row.CreationTimestamp = ts.UTC().Format(time.RFC3339)
	}
	return row
}

// shapeEventFeedRow shapes a core Event into a feed row, reusing the shared
// last-seen/count resolution so a series event reads the same as elsewhere.
func shapeEventFeedRow(e *corev1.Event) EventFeedRow {
	return EventFeedRow{
		Name:              e.Name,
		Namespace:         e.Namespace,
		UID:               string(e.UID),
		CreationTimestamp: formatTimestamp(e.CreationTimestamp),
		Type:              e.Type,
		Reason:            e.Reason,
		Message:           e.Message,
		Count:             eventCount(e),
		LastSeen:          formatTimestamp(eventLastSeen(e)),
		InvolvedObject: InvolvedObjectRef{
			Kind:      e.InvolvedObject.Kind,
			Namespace: e.InvolvedObject.Namespace,
			Name:      e.InvolvedObject.Name,
		},
	}
}

// fromUnstructured converts an unstructured object into a typed one, reporting
// false on failure so the caller falls back to the generic row rather than
// emitting a zero-valued typed summary.
func fromUnstructured(u *unstructured.Unstructured, into any) bool {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, into) == nil
}
