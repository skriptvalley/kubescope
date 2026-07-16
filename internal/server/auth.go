package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
)

// healthzPath is exempt from both auth and the Host allowlist: liveness/readiness
// probes must reach it without credentials and regardless of the Host header they
// carry (kubelet/docker probes often use a pod IP or container name).
const healthzPath = "/healthz"

// authGuard builds the authentication middleware selected by KUBESCOPE_AUTH_MODE
// (ADR-0005). Modes:
//
//   - "none" (and any non-"basic" value): pass-through. `oidc` never reaches
//     here — config.Load fails fast on it at startup.
//   - "basic": every route except /healthz requires HTTP Basic credentials that
//     match username/password. Missing or wrong credentials get a 401 with a
//     WWW-Authenticate challenge (so a browser prompts, and the SPA is gated too).
//
// Credentials are compared in constant time (SHA-256 of each side, so neither the
// value nor its length leaks via timing) and are never logged. A failed attempt
// logs only the path, remote address, and whether any credentials were presented
// — never the submitted username or password.
func authGuard(mode, username, password string, logger *slog.Logger) func(http.Handler) http.Handler {
	if mode != "basic" {
		return func(next http.Handler) http.Handler { return next }
	}

	wantUser := sha256.Sum256([]byte(username))
	wantPass := sha256.Sum256([]byte(password))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == healthzPath {
				next.ServeHTTP(w, r)
				return
			}

			user, pass, ok := r.BasicAuth()
			if ok {
				gotUser := sha256.Sum256([]byte(user))
				gotPass := sha256.Sum256([]byte(pass))
				// Evaluate both comparisons unconditionally: no early return, so
				// the check does not leak which half was wrong.
				userOK := subtle.ConstantTimeCompare(gotUser[:], wantUser[:]) == 1
				passOK := subtle.ConstantTimeCompare(gotPass[:], wantPass[:]) == 1
				if userOK && passOK {
					next.ServeHTTP(w, r)
					return
				}
			}

			logger.Warn("auth rejected",
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"had_credentials", ok,
			)
			w.Header().Set("WWW-Authenticate", `Basic realm="Kubescope", charset="UTF-8"`)
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		})
	}
}
