package server

import (
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
)

// spaHandler serves the embedded frontend build. Requests matching a real
// file are served as-is; anything else gets index.html so client-side routes
// deep-link correctly. /api and /healthz never reach this handler — they are
// routed first. basePath ("" or "/kubescope") becomes the index's <base href>.
func spaHandler(dist fs.FS, basePath string) http.Handler {
	fileServer := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" && path != "index.html" {
			if info, err := fs.Stat(dist, path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, r, dist, basePath)
	})
}

// serveIndex writes index.html without the FileServer redirect dance. A build
// without an embedded frontend (placeholder dist) is a 503, not a panic.
func serveIndex(w http.ResponseWriter, r *http.Request, dist fs.FS, basePath string) {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		http.Error(w, "frontend not embedded in this build — run `make build`", http.StatusServiceUnavailable)
		return
	}
	index = withBaseHref(index, basePath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The SPA shell must not be cached: hashed assets change under it.
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(index)
}

// headOpen matches the <head> start tag, with or without attributes.
var headOpen = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)

// withBaseHref inserts <base href="<basePath>/"> as the first element of <head>
// (ADR-0012). The build emits relative asset URLs (Vite base "./"), so this one
// tag makes them — and the SPA's router basename and API/SSE/WebSocket URLs,
// which read it back — resolve under the mount point from any deep link. It is
// always written, "/" at the root, since a relative asset URL on a deep link
// (/resources/core/v1/pods) would otherwise resolve under that route.
func withBaseHref(index []byte, basePath string) []byte {
	tag := []byte(`<base href="` + html.EscapeString(basePath+"/") + `">`)
	loc := headOpen.FindIndex(index)
	if loc == nil {
		return index // not a normal document shell; serve untouched
	}
	at := loc[1]
	out := make([]byte, 0, len(index)+len(tag))
	out = append(out, index[:at]...)
	out = append(out, tag...)
	return append(out, index[at:]...)
}
