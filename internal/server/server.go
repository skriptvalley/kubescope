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
)

// Options carries the dependencies the router needs; all injected, no
// globals.
type Options struct {
	Logger *slog.Logger
	// Kube enumerates/switches contexts and provides clientsets for API
	// handlers (a superset of resources.ClientsetProvider).
	Kube resources.Cluster
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
			// Generic resource engine: any GVR via the dynamic client. The
			// core group travels as the literal token "core" in the path.
			v1.Get("/resources/{group}/{version}/{resource}", resources.ListHandler(opts.Kube, disco, opts.Logger))
			v1.Get("/resources/{group}/{version}/{resource}/{name}", resources.GetHandler(opts.Kube, disco, opts.Logger))
			v1.Get("/resources/{group}/{version}/{resource}/{name}/yaml", resources.YAMLHandler(opts.Kube, disco, opts.Logger))
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
