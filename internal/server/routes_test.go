package server

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mutatingMethods are the HTTP methods that can change cluster state and must
// therefore sit behind the read-only guard — unless a route is explicitly
// classified as non-mutating below.
var mutatingMethods = map[string]bool{
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

// guardedRoutes is the full set of state-mutating routes that MUST be rejected in
// read-only mode (ADR-0005). Keyed "METHOD PATTERN".
var guardedRoutes = map[string]bool{
	"POST /api/v1/kubeconfigs":                                     true,
	"DELETE /api/v1/kubeconfigs/{id}":                              true,
	"PUT /api/v1/resources/{group}/{version}/{resource}/{name}":    true,
	"DELETE /api/v1/resources/{group}/{version}/{resource}/{name}": true,
	"POST /api/v1/workloads/{resource}/{namespace}/{name}/scale":   true,
	"POST /api/v1/workloads/{resource}/{namespace}/{name}/restart": true,
	"POST /api/v1/nodes/{name}/cordon":                             true,
	"POST /api/v1/nodes/{name}/uncordon":                           true,
	"POST /api/v1/nodes/{name}/drain":                              true,
	"POST /api/v1/portforwards":                                    true,
}

// exemptMutatingRoutes are the mutating-method routes that deliberately stay
// usable in read-only mode because they change no cluster state: the in-memory
// context switch, and stopping a backend-local port-forward listener the user
// already started (Sprint 6). Any new mutating-method route must be classified
// into one of these two sets or TestMutatingRouteSurfaceIsClassified fails.
var exemptMutatingRoutes = map[string]bool{
	"POST /api/v1/contexts/switch":     true,
	"DELETE /api/v1/portforwards/{id}": true,
}

// TestMutatingRouteSurfaceIsClassified walks the actual router and asserts every
// registered mutating-method route is accounted for — either read-only-guarded or
// explicitly exempt. A new mutating route added outside the guard (a security
// bug) makes this fail, forcing a conscious classification decision.
func TestMutatingRouteSurfaceIsClassified(t *testing.T) {
	routes, ok := readOnlyServer(false).(chi.Routes)
	require.True(t, ok, "router must expose chi.Routes for walking")

	found := map[string]bool{}
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if mutatingMethods[method] {
			found[method+" "+route] = true
		}
		return nil
	})
	require.NoError(t, err)

	for key := range found {
		if guardedRoutes[key] || exemptMutatingRoutes[key] {
			continue
		}
		t.Errorf("unclassified mutating route %q: register it under the read-only guard, or add it to exemptMutatingRoutes with a reason", key)
	}

	// Every guarded route we expect must actually be registered — catches a route
	// being renamed or dropped without updating the guard contract.
	for key := range guardedRoutes {
		assert.True(t, found[key], "expected guarded route %q to be registered", key)
	}
}
