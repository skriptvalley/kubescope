// Package stream bridges Kubernetes watch and pod-log streams into per-context
// Server-Sent Events fan-out, exec sessions over WebSocket, and backend-managed
// port-forwards (ADR-0006). A shared informer per context+GVR feeds every SSE
// subscriber; pod logs follow over the same SSE transport; the exec terminal
// rides a WebSocket bridged to the SPDY exec API; port-forwards are a plain HTTP
// start/stop/list API over a client-go SPDY port-forward registry. Exec sessions
// and port-forwards are per-context and torn down on a context switch or server
// shutdown — they never outlive their context.
package stream

// EventType classifies a watch event on the SSE wire.
type EventType string

const (
	// EventAdd / EventUpdate carry a shaped row (and, for detail subscribers,
	// the full object). EventDelete carries only the object's identity.
	EventAdd    EventType = "add"
	EventUpdate EventType = "update"
	EventDelete EventType = "delete"
	// EventResync tells the client its view may have gaps (its buffer overflowed,
	// or the cluster just recovered from an outage) and it should refetch a clean
	// baseline. It carries no payload.
	EventResync EventType = "resync"
	// EventStatus reports a change in cluster reachability for this GVR's watch:
	// State "unreachable" when the shared informer's watch has been failing, and
	// "connected" once a recovery probe succeeds again. It carries a StatusInfo.
	// Emitted once per transition (repeated failures are dampened, FB-6).
	EventStatus EventType = "status"
)

// StatusInfo is the payload of an EventStatus frame: the connectivity transition
// (unreachable/connected) plus a classified reason, sanitized error message and
// actionable remediation the UI surfaces in a banner (FB-6, Story D).
type StatusInfo struct {
	State    string `json:"state"`              // "unreachable" | "connected"
	Reason   string `json:"reason,omitempty"`   // kube.FailureClass
	Message  string `json:"message,omitempty"`  // sanitized error
	Guidance string `json:"guidance,omitempty"` // remediation
}

// ObjectRef is the minimal identity of an object, used for delete events where
// the frontend only needs to drop the matching row/detail.
type ObjectRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	UID       string `json:"uid,omitempty"`
}

// Event is one watch notification as delivered to a subscriber and serialized
// onto the SSE stream. Row is the server-shaped list/feed row (add/update);
// Object is the full object, included only for detail subscribers; Ref is the
// identity for deletes. Resync events carry none of these.
type Event struct {
	Type   EventType      `json:"type"`
	Row    any            `json:"row,omitempty"`
	Object map[string]any `json:"object,omitempty"`
	Ref    *ObjectRef     `json:"ref,omitempty"`
	Status *StatusInfo    `json:"status,omitempty"`
}
