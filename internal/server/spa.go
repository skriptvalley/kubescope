package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves the embedded frontend build. Requests matching a real
// file are served as-is; anything else gets index.html so client-side routes
// deep-link correctly. /api and /healthz never reach this handler — they are
// routed first.
func spaHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" && path != "index.html" {
			if info, err := fs.Stat(dist, path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, r, dist)
	})
}

// serveIndex writes index.html without the FileServer redirect dance. A build
// without an embedded frontend (placeholder dist) is a 503, not a panic.
func serveIndex(w http.ResponseWriter, r *http.Request, dist fs.FS) {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		http.Error(w, "frontend not embedded in this build — run `make build`", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The SPA shell must not be cached: hashed assets change under it.
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(index)
}
