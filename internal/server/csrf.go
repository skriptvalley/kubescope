package server

import "net/http"

// crossOriginGuard rejects non-safe cross-origin browser requests with Go's
// CrossOriginProtection (Sec-Fetch-Site, else Origin host vs Host). Kubescope's
// credentials are ambient either way — cached Basic credentials, or a session
// cookie checked by an authenticating proxy in front (ADR-0012) — and several
// mutations are body-less POSTs (restart, cordon, drain) that a plain HTML form on
// another site could submit. SameSite=Lax cookies don't stop a sibling subdomain
// (same-site), so the check is on origin, not site. GET/HEAD/OPTIONS always pass;
// the exec WebSocket handshake (a GET) verifies Origin itself. Requests without
// either header (curl, scripts) are not browsers and pass to auth as before.
func crossOriginGuard() func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusForbidden, "cross_origin_rejected",
			"cross-origin request rejected (CSRF protection): use Kubescope from its own origin")
	}))
	return cop.Handler
}
