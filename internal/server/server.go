// Package server wires the chi router: /healthz, the /api tree and the
// embedded SPA with fallback serving (ADR-0002).
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/skriptvalley/kubescope/internal/resources"
	"github.com/skriptvalley/kubescope/internal/stream"
)

// StreamCluster is the context/client surface the streaming handlers need:
// per-context dynamic client (watch bridge) and typed clientset (pod logs).
// *kube.Manager satisfies it (ADR-0006).
type StreamCluster interface {
	stream.Cluster
	stream.LogCluster
}

// Options carries the dependencies the router needs; all injected, no
// globals.
type Options struct {
	Logger *slog.Logger
	// Kube enumerates/switches contexts and provides clientsets for API
	// handlers (a superset of resources.ClientsetProvider).
	Kube resources.Cluster
	// Stream backs the watch→SSE and pod-log endpoints (ADR-0006). When nil,
	// the streaming routes are not registered (used by router-only tests).
	Stream StreamCluster
	// Exec is the cluster surface for the exec WebSocket bridge (Sprint 6):
	// clientset + rest.Config per context. When nil (or ExecSessions is nil) the
	// exec route is not registered. *kube.Manager satisfies it.
	Exec stream.ExecCluster
	// ExecSessions tracks live exec sessions so a context switch or shutdown can
	// tear them down; shared with main so shutdown can close them all.
	ExecSessions *stream.ExecRegistry
	// PortForwards backs the port-forward start/stop/list API (Sprint 6). When
	// nil those routes are not registered. Shared with main for shutdown teardown.
	PortForwards *stream.PortForwardManager
	// ReadOnly rejects every mutating route with a 403 via server-side
	// middleware — the bypass-proof guardrail from ADR-0005. The UI reads the
	// same flag from /api/v1/config to disable controls, but this is the control.
	ReadOnly bool
	// AuthMode is surfaced to the frontend via /api/v1/config (none|basic) and
	// selects the auth middleware. "basic" gates every route (except /healthz)
	// with HTTP Basic auth using the credentials below (ADR-0005).
	AuthMode string
	// BasicAuthUsername/BasicAuthPassword are enforced when AuthMode is "basic".
	// Never logged.
	BasicAuthUsername string
	BasicAuthPassword string
	// ListenAddr is the configured bind address; the Host-allowlist middleware
	// (DNS-rebinding protection, FB-3) derives its allowlist from it. Empty
	// disables the Host check (used by router-only tests).
	ListenAddr string
	// Dist is the built SPA (index.html at its root).
	Dist fs.FS
}

// New builds the top-level HTTP handler.
func New(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(opts.Logger))
	r.Use(middleware.Recoverer)
	// Host-allowlist (FB-3) then auth (ADR-0005) run ahead of every route so a
	// rebinding request is dropped, then an unauthenticated one is challenged,
	// before any handler — including the SPA — is reached. /healthz is exempt
	// from both.
	r.Use(hostGuard(opts.ListenAddr))
	r.Use(authGuard(opts.AuthMode, opts.BasicAuthUsername, opts.BasicAuthPassword, opts.Logger))

	r.Get("/healthz", healthz)

	r.Route("/api", func(api chi.Router) {
		// Unknown /api paths are JSON 404s, never the SPA fallback.
		api.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeJSONError(w, http.StatusNotFound, "not_found", "no such API route")
		})
		// Wrong-method /api requests get the same JSON envelope, not chi's
		// bare empty-body 405.
		api.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this API route")
		})
		// One discovery service per server: caches each context's API
		// enumeration and is shared by the discovery and generic get/list
		// handlers so GVR resolution reuses the same cache (ADR-0003).
		disco := resources.NewDiscoveryService(opts.Kube)
		api.Route("/v1", func(v1 chi.Router) {
			v1.Get("/nodes", resources.NodesHandler(opts.Kube, opts.Logger))
			v1.Get("/contexts", resources.ContextsHandler(opts.Kube, opts.Logger))
			v1.Post("/contexts/switch", resources.SwitchContextHandler(opts.Kube, opts.Logger))
			v1.Get("/contexts/health", resources.HealthHandler(opts.Kube, opts.Logger))
			v1.Get("/overview", resources.OverviewHandler(opts.Kube, opts.Logger))
			v1.Get("/namespaces", resources.NamespacesHandler(opts.Kube, opts.Logger))
			v1.Get("/discovery", resources.DiscoveryHandler(disco, opts.Kube, opts.Logger))
			// Server posture the UI reflects (read-only, auth mode). Touches no
			// cluster, so it answers even when no cluster is reachable (ADR-0005).
			v1.Get("/config", resources.ConfigHandler(
				resources.ServerConfig{ReadOnly: opts.ReadOnly, AuthMode: opts.AuthMode}, opts.Logger))
			// Generic resource engine: any GVR via the dynamic client. The
			// core group travels as the literal token "core" in the path.
			v1.Get("/resources/{group}/{version}/{resource}", resources.ListHandler(opts.Kube, disco, opts.Logger))
			v1.Get("/resources/{group}/{version}/{resource}/{name}", resources.GetHandler(opts.Kube, disco, opts.Logger))
			v1.Get("/resources/{group}/{version}/{resource}/{name}/yaml", resources.YAMLHandler(opts.Kube, disco, opts.Logger))
			// Typed workload summaries + controller drill-down (Sprint 3):
			// the hot-path complement to the generic engine (ADR-0003).
			v1.Get("/workloads/{resource}", resources.WorkloadListHandler(opts.Kube, opts.Logger))
			v1.Get("/workloads/{resource}/{namespace}/{name}/pods", resources.OwnedPodsHandler(opts.Kube, opts.Logger))
			v1.Get("/workloads/{resource}/{namespace}/{name}/jobs", resources.OwnedJobsHandler(opts.Kube, opts.Logger))
			v1.Get("/events", resources.EventsHandler(opts.Kube, opts.Logger))
			// Cluster-wide/per-namespace events feed (Sprint 4): initial-paint +
			// polling-fallback complement to the live events stream below.
			v1.Get("/events/feed", resources.EventsFeedHandler(opts.Kube, opts.Logger))
			// Per-key Secret reveal: the one read that returns a real Secret value,
			// one key at a time on explicit action (ADR-0005). A read, so it is not
			// gated by read-only mode.
			v1.Get("/secrets/{namespace}/{name}/reveal", resources.RevealSecretHandler(opts.Kube, opts.Logger))
			// Typed Service detail (Sprint 7): the Service summary plus its resolved
			// Endpoints (ready/not-ready backing pods) — the hot-path complement to
			// the generic object the detail view already fetches.
			v1.Get("/services/{namespace}/{name}", resources.ServiceDetailHandler(opts.Kube, opts.Logger))
			// Global search (Sprint 7): name matches across the active context's
			// discovered types, bounded and partial-tolerant. Shares the discovery
			// cache so it reuses the same GVR enumeration as the nav.
			v1.Get("/search", resources.SearchHandler(opts.Kube, disco, opts.Logger))

			// Port-forward list + stop (Sprint 6). Both manage backend-local
			// session state — a loopback listener the user already started — not
			// cluster state, so they stay usable in read-only mode; only starting a
			// forward is gated (in the mutation group below).
			if opts.PortForwards != nil {
				v1.Get("/portforwards", opts.PortForwards.ListHandler())
				v1.Delete("/portforwards/{id}", opts.PortForwards.DeleteHandler())
			}

			// Mutating routes (Sprint 5). Registered behind read-only middleware so
			// KUBESCOPE_READ_ONLY=true returns 403 for every one of them, server-
			// side and bypass-proof — a direct API call is rejected the same as the
			// UI (ADR-0005). Every mutation lives here; nothing mutating is
			// registered outside this group.
			v1.Group(func(m chi.Router) {
				m.Use(readOnlyGuard(opts.ReadOnly))
				m.Put("/resources/{group}/{version}/{resource}/{name}", resources.UpdateHandler(opts.Kube, disco, opts.Logger))
				m.Delete("/resources/{group}/{version}/{resource}/{name}", resources.DeleteHandler(opts.Kube, disco, opts.Logger))
				m.Post("/workloads/{resource}/{namespace}/{name}/scale", resources.ScaleHandler(opts.Kube, opts.Logger))
				m.Post("/workloads/{resource}/{namespace}/{name}/restart", resources.RestartHandler(opts.Kube, opts.Logger))
				m.Post("/nodes/{name}/cordon", resources.CordonHandler(opts.Kube, opts.Logger))
				m.Post("/nodes/{name}/uncordon", resources.UncordonHandler(opts.Kube, opts.Logger))
				m.Post("/nodes/{name}/drain", resources.DrainHandler(opts.Kube, opts.Logger))
				// Exec runs arbitrary in-pod commands and starting a port-forward
				// opens a tunnel to a workload — both mutating capabilities, so
				// read-only mode 403s them here. Exec is a WebSocket upgrade
				// (ADR-0006); the guard rejects the request before it upgrades.
				if opts.Exec != nil && opts.ExecSessions != nil {
					m.Get("/stream/pods/{namespace}/{name}/exec", stream.ExecHandler(opts.Exec, opts.ExecSessions, opts.Logger))
				}
				if opts.PortForwards != nil {
					m.Post("/portforwards", opts.PortForwards.CreateHandler())
				}
			})

			// Live updates (Sprint 4, ADR-0006): a shared informer per
			// context+GVR fans watch events out over SSE, and pod logs follow
			// over the same transport. Registered only when a stream backend is
			// wired (production always is; router-only tests skip it).
			if opts.Stream != nil {
				// The object sanitizer masks Secret data on detail streams so a
				// watched Secret is redacted server-side, matching the REST views
				// (ADR-0005) — the SSE detail object is a render path too.
				hub := stream.NewHub(opts.Stream, resources.ShapeStreamRow, opts.Logger,
					stream.WithObjectSanitizer(resources.MaskStreamObject))
				v1.Get("/stream/resources/{group}/{version}/{resource}", stream.StreamHandler(hub, opts.Logger))
				v1.Get("/stream/pods/{namespace}/{name}/logs", stream.LogsHandler(opts.Stream, opts.Logger))
			}
		})
	})

	// Everything else is the SPA: real files as-is, unknown paths fall back
	// to index.html for client-side routing.
	r.NotFound(spaHandler(opts.Dist).ServeHTTP)

	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]map[string]string{
		"error": {"code": code, "message": message},
	})
}

// requestLogger emits one structured line per request via slog.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
				"remote", r.RemoteAddr,
			)
		})
	}
}
