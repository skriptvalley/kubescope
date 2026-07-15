package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/skriptvalley/kubescope/internal/stream"
)

// mutatingRoutes enumerates every mutating API route. This list is the read-only
// guardrail's contract (ADR-0005): each must 403 when read-only is on and pass
// through when it is off. A new mutating route that is not added here — and thus
// not registered inside the read-only group — is a security bug this test exists
// to catch, so keep it exhaustive.
var mutatingRoutes = []struct {
	name   string
	method string
	path   string
	body   string
}{
	{"apply", http.MethodPut, "/api/v1/resources/apps/v1/deployments/nginx?namespace=default", `{"yaml":"kind: Deployment"}`},
	{"delete", http.MethodDelete, "/api/v1/resources/apps/v1/deployments/nginx?namespace=default", ""},
	{"scale", http.MethodPost, "/api/v1/workloads/deployments/default/nginx/scale", `{"replicas":3}`},
	{"rollout-restart", http.MethodPost, "/api/v1/workloads/deployments/default/nginx/restart", ""},
	{"cordon", http.MethodPost, "/api/v1/nodes/node-1/cordon", ""},
	{"uncordon", http.MethodPost, "/api/v1/nodes/node-1/uncordon", ""},
	{"drain", http.MethodPost, "/api/v1/nodes/node-1/drain", ""},
	// Sprint 6: exec (a WebSocket upgrade, guarded before it upgrades) and
	// starting a port-forward. The port-forward body deliberately omits `pod` so
	// that in writable mode the handler fails validation (400) *before* any
	// cluster dial — proving the guard passes through without a live cluster.
	{"exec", http.MethodGet, "/api/v1/stream/pods/default/nginx/exec?container=web", ""},
	{"port-forward-start", http.MethodPost, "/api/v1/portforwards", `{"namespace":"default","remotePort":80}`},
}

func readOnlyServer(readOnly bool) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := &fakeProvider{clientset: fake.NewClientset()}
	return New(Options{
		Logger:       logger,
		Kube:         provider,
		Exec:         provider,
		ExecSessions: stream.NewExecRegistry(),
		PortForwards: stream.NewPortForwardManager(provider, logger),
		ReadOnly:     readOnly,
		Dist:         fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("spa")}},
	})
}

func doMutation(t *testing.T, srv http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

// TestReadOnlyRejectsEveryMutatingRoute is the enumerated guardrail test: with
// read-only on, every mutating route returns a structured 403 regardless of the
// (fake) cluster, and the rejection is server-side — no frontend involved.
func TestReadOnlyRejectsEveryMutatingRoute(t *testing.T) {
	srv := readOnlyServer(true)
	for _, route := range mutatingRoutes {
		t.Run(route.name, func(t *testing.T) {
			rec := doMutation(t, srv, route.method, route.path, route.body)
			assert.Equal(t, http.StatusForbidden, rec.Code, "read-only must reject %s %s", route.method, route.path)

			var env struct {
				Error struct{ Code string } `json:"error"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			assert.Equal(t, "read_only", env.Error.Code)
		})
	}
}

// TestWritableAllowsEveryMutatingRoute is the pass-through half: with read-only
// off, none of the mutating routes are short-circuited to 403 — they reach their
// handlers (which then fail against the fake cluster for other reasons). A 403
// here would mean the guard is stuck on regardless of config.
func TestWritableAllowsEveryMutatingRoute(t *testing.T) {
	srv := readOnlyServer(false)
	for _, route := range mutatingRoutes {
		t.Run(route.name, func(t *testing.T) {
			rec := doMutation(t, srv, route.method, route.path, route.body)
			assert.NotEqual(t, http.StatusForbidden, rec.Code,
				"writable mode must not 403 %s %s (got %d)", route.method, route.path, rec.Code)
		})
	}
}

// TestConfigEndpointReflectsReadOnly verifies the UI-facing config mirror carries
// the same flag the middleware enforces, and that reads are never gated by it.
func TestConfigEndpointReflectsReadOnly(t *testing.T) {
	for _, ro := range []bool{true, false} {
		srv := readOnlyServer(ro)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var cfg struct {
			ReadOnly bool   `json:"readOnly"`
			AuthMode string `json:"authMode"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cfg))
		assert.Equal(t, ro, cfg.ReadOnly)
	}
}

// TestContextSwitchNotGatedByReadOnly guards the classification boundary: the
// in-memory context switch mutates no cluster state, so it must stay usable in
// read-only mode (a read-only user still browses across clusters).
func TestContextSwitchNotGatedByReadOnly(t *testing.T) {
	srv := readOnlyServer(true)
	rec := doMutation(t, srv, http.MethodPost, "/api/v1/contexts/switch", `{"name":"test"}`)
	assert.NotEqual(t, http.StatusForbidden, rec.Code, "context switch is not a cluster mutation")
}
