package server

import (
	"net"
	"net/http"
	"strings"
)

// hostGuard defends against DNS-rebinding (FB-3): a page the victim's browser
// loads at attacker.example whose DNS is flipped to a loopback address can reach
// a localhost-bound Kubescope, but its requests still carry the attacker's Host
// header. Rejecting Host values outside an allowlist closes that path — an
// Origin/loopback-bind check alone cannot, because the rebinding request looks
// same-origin to the socket.
//
// The allowlist is derived from the configured bind address (FB-3): the loopback
// names plus the concrete bind host, matched with or without a port. /healthz is
// exempt so probes are never rejected on Host grounds.
//
// When the server binds a wildcard address (0.0.0.0 / :: — the Docker image
// default, an explicit "expose me" choice), Host values are unpredictable (LAN
// IP, container name, reverse-proxy name) and the operative protection is auth +
// network controls per ADR-0005, so the guard is a pass-through in that mode
// rather than break legitimate access.
func hostGuard(listenAddr string) func(http.Handler) http.Handler {
	allowed := allowedHosts(listenAddr)
	if allowed == nil {
		// Wildcard/unknown bind: no enforceable allowlist — pass through.
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == healthzPath {
				next.ServeHTTP(w, r)
				return
			}
			if hostAllowed(r.Host, allowed) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSONError(w, http.StatusForbidden, "forbidden_host",
				"request Host is not allowed (DNS-rebinding protection); reach Kubescope via localhost or its configured address")
		})
	}
}

// allowedHosts returns the set of accepted Host hostnames (port-stripped, lower-
// cased) for a bind address, or nil when the bind is a wildcard and no meaningful
// allowlist exists. Loopback names are always included; a concrete bind host is
// added so binding an explicit IP/name keeps that address reachable.
func allowedHosts(listenAddr string) map[string]bool {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		// No host:port to key off (e.g. empty in tests): nothing to enforce.
		return nil
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if isWildcardHost(host) {
		return nil
	}
	allowed := map[string]bool{
		"localhost": true,
		"127.0.0.1": true,
		"::1":       true,
	}
	if host != "" {
		allowed[host] = true
	}
	return allowed
}

// isWildcardHost reports whether a bind host means "all interfaces" and thus has
// no fixed hostname to allowlist.
func isWildcardHost(host string) bool {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	default:
		return false
	}
}

// hostAllowed reports whether an incoming request Host (which may include a port
// and, for IPv6, brackets) is in the allowlist. An empty Host is allowed: only
// browsers are exposed to rebinding and they always send one, so a missing Host
// is a non-browser client (curl/HTTP-1.0) that the loopback bind already fences.
func hostAllowed(reqHost string, allowed map[string]bool) bool {
	if reqHost == "" {
		return true
	}
	host := reqHost
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	return allowed[host]
}
