package stream

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// defaultHeartbeat is how often an idle stream emits a comment to keep the
// connection alive through proxies, and how often it checks whether its bound
// context is still active.
const defaultHeartbeat = 15 * time.Second

// coreGroupToken is the URL segment standing in for the empty core API group,
// matching the generic REST engine's convention.
const coreGroupToken = "core"

// heartbeatComment is an SSE comment line; clients ignore it, proxies see bytes.
var heartbeatComment = []byte(": ping\n\n")

// handlerConfig holds tunables for the SSE handlers (heartbeat cadence).
type handlerConfig struct {
	heartbeat time.Duration
}

// HandlerOption tunes an SSE handler.
type HandlerOption func(*handlerConfig)

// WithHeartbeat overrides the keep-alive/context-check cadence.
func WithHeartbeat(d time.Duration) HandlerOption {
	return func(c *handlerConfig) { c.heartbeat = d }
}

// StreamHandler serves GET /api/v1/stream/resources/{group}/{version}/{resource}:
// a live SSE feed of add/update/delete events for the GVR in the active
// context. `?namespace=` and `?name=` narrow the feed; `?detail=true` includes
// the full object (for detail views). Watch errors surface as an explicit
// resync event; the stream closes when the active context moves away (ADR-0006).
func StreamHandler(hub *Hub, logger *slog.Logger, opts ...HandlerOption) http.HandlerFunc {
	cfg := handlerConfig{heartbeat: defaultHeartbeat}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeStreamError(w, logger, http.StatusInternalServerError, "streaming_unsupported",
				"the server cannot stream responses")
			return
		}

		gvr := gvrFromStreamRequest(r)
		q := r.URL.Query()
		filter := Filter{
			Namespace:     q.Get("namespace"),
			Name:          q.Get("name"),
			IncludeObject: q.Get("detail") == "true",
		}

		sub, err := hub.Subscribe(gvr, filter)
		if err != nil {
			writeStreamError(w, logger, http.StatusServiceUnavailable, "stream_unavailable",
				fmt.Sprintf("cannot open stream for %s: %v", gvr.Resource, err))
			return
		}
		defer sub.Close()

		runSSE(w, r, flusher, logger, hub, sub, cfg.heartbeat)
	}
}

// runSSE writes the SSE headers and then pumps events, heartbeats and resync
// signals until the client disconnects or the bound context is switched away.
func runSSE(w http.ResponseWriter, r *http.Request, flusher http.Flusher, logger *slog.Logger, hub *Hub, sub *Subscription, heartbeat time.Duration) {
	setSSEHeaders(w)
	flusher.Flush() // establish the stream immediately so the client goes "live"

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	ctx := r.Context()

	for {
		// A connectivity transition (cluster went unreachable, or recovered) is
		// surfaced as a status frame so the UI can show/hide its banner. Checked
		// here — after every heartbeat tick and event send — like resync.
		if info := sub.TakeStatus(); info != nil {
			if !writeEvent(w, flusher, logger, Event{Type: EventStatus, Status: info}) {
				return
			}
		}
		// A pending resync supersedes buffered events: the client will refetch a
		// clean baseline, so any events still queued (older than the ones dropped
		// on overflow) are stale and must be discarded, not applied over the
		// fresh baseline.
		if sub.TakeResync() {
			drainEvents(sub)
			if !writeEvent(w, flusher, logger, Event{Type: EventResync}) {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Enforce ADR-0006: a stream bound to a now-inactive context closes.
			if current, err := hub.cluster.ActiveContextName(); err != nil || current != sub.Context() {
				return
			}
			if _, err := w.Write(heartbeatComment); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-sub.Events():
			if !writeEvent(w, flusher, logger, ev) {
				return
			}
		}
	}
}

// drainEvents non-blockingly discards every event currently queued for the
// subscriber. Called after a resync so known-stale backlog is never applied
// over the clean baseline the client is about to refetch.
func drainEvents(sub *Subscription) {
	for {
		select {
		case <-sub.Events():
		default:
			return
		}
	}
}

// writeEvent serializes one event as an SSE `data:` frame and flushes. It
// returns false only on a write error (client gone) so the caller stops; a
// marshal error is logged and skipped without tearing the stream down.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, logger *slog.Logger, ev Event) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		logger.Error("marshaling stream event", "error", err)
		return true
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // defeat proxy buffering of SSE
	w.WriteHeader(http.StatusOK)
}

// writeStreamError writes a structured JSON error before any SSE headers exist
// (subscribe failed). EventSource treats it as a connection error and retries.
func writeStreamError(w http.ResponseWriter, logger *slog.Logger, status int, code, message string) {
	writeStreamErrorClassified(w, logger, status, code, message, "", "")
}

// writeStreamErrorClassified is writeStreamError plus the optional classifier
// output (remediation and doc link) so a pre-open connectivity failure carries
// the same actionable fields as the REST error envelope. Empty guidance/docURL
// are omitted, keeping the simple errors byte-identical to writeStreamError.
func writeStreamErrorClassified(w http.ResponseWriter, logger *slog.Logger, status int, code, message, guidance, docURL string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	e := map[string]string{"code": code, "message": message}
	if guidance != "" {
		e["guidance"] = guidance
	}
	if docURL != "" {
		e["docURL"] = docURL
	}
	if err := json.NewEncoder(w).Encode(map[string]map[string]string{"error": e}); err != nil {
		logger.Error("encoding stream error", "error", err)
	}
}

// gvrFromStreamRequest builds the GVR from the route, mapping the core-group
// token back to the empty group.
func gvrFromStreamRequest(r *http.Request) schema.GroupVersionResource {
	group := chi.URLParam(r, "group")
	if group == coreGroupToken {
		group = ""
	}
	return schema.GroupVersionResource{
		Group:    group,
		Version:  chi.URLParam(r, "version"),
		Resource: chi.URLParam(r, "resource"),
	}
}
