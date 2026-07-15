// Package stream bridges Kubernetes watch and pod-log streams into per-context
// Server-Sent Events fan-out (ADR-0006). A shared informer per context+GVR
// feeds every subscriber; pod logs follow over the same SSE transport. The
// exec WebSocket bridge (ADR-0006) lands in Sprint 6.
package stream

// EventType classifies a watch event on the SSE wire.
type EventType string

const (
	// EventAdd / EventUpdate carry a shaped row (and, for detail subscribers,
	// the full object). EventDelete carries only the object's identity.
	EventAdd    EventType = "add"
	EventUpdate EventType = "update"
	EventDelete EventType = "delete"
	// EventResync tells the client its view may have gaps (a watch error forced
	// a re-list, or its buffer overflowed) and it should refetch a clean
	// baseline. It carries no payload.
	EventResync EventType = "resync"
)

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
}
