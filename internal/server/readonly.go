package server

import "net/http"

// readOnlyGuard is the server-side read-only enforcement from ADR-0005. When
// read-only mode is on it rejects every request in the group it wraps with a
// 403 — before the handler runs, so no cluster mutation can occur. It is applied
// to the mutation route group only; read routes and the in-memory context switch
// are unaffected. Because enforcement is here, not in the UI, a direct API call
// (curl, a script) is rejected identically to a click in the browser.
//
// When read-only mode is off the middleware is a pass-through, so the same route
// tree serves both modes with no per-handler branching.
func readOnlyGuard(readOnly bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !readOnly {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusForbidden, "read_only",
				"Kubescope is running in read-only mode (KUBESCOPE_READ_ONLY=true); mutating operations are disabled")
		})
	}
}
