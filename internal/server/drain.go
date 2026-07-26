package server

import (
	"context"
	"net/http"
)

// drainGuard cancels a request's context once the server starts shutting down.
//
// It exists for the long-lived streaming routes. An SSE stream (a watch feed or
// a followed pod log) is one HTTP request that only ends when the client goes
// away, and http.Server.Shutdown waits for every active request — so with a
// browser tab open, shutdown would block until its timeout expired and then
// report a deadline error, leaving the listener bound the whole time. The
// streaming handlers already unwind on request-context cancellation, so
// cancelling here lets Shutdown finish as soon as the short-lived requests
// drain.
//
// Only long-lived routes are wrapped: ordinary API requests complete in
// milliseconds and are left to finish normally, so an in-flight mutation is
// never cut short by shutdown. A nil channel disables the guard (router-only
// tests, which never shut down).
func drainGuard(drain <-chan struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if drain == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			// Bounded by the number of live streams and released with the
			// request: whichever of the two fires first ends the goroutine.
			defer cancel()
			go func() {
				select {
				case <-drain:
					cancel()
				case <-ctx.Done():
				}
			}()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
