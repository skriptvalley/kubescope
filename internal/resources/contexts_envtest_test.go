package resources_test

// Envtest-backed integration test for Sprint 1: boots a real kube-apiserver and
// mounts it as one context in a two-context kubeconfig (the second is an
// unreachable stub). Exercises the full chain kubeconfig → kube.Manager →
// router for /contexts, /contexts/switch, /contexts/health and /overview.
//
// Requires KUBEBUILDER_ASSETS (make test sets it); skipped otherwise.

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/resources"
	"github.com/skriptvalley/kubescope/internal/server"
)

// writeTwoContextKubeconfig writes a kubeconfig with the live envtest context
// plus an unreachable "bogus" context, current-context = envtest.
func writeTwoContextKubeconfig(t *testing.T, cfg *rest.Config) string {
	t.Helper()
	kc := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"envtest": {Server: cfg.Host, CertificateAuthorityData: cfg.CAData},
			"bogus":   {Server: "https://127.0.0.1:1", InsecureSkipTLSVerify: true},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"envtest": {
				ClientCertificateData: cfg.CertData,
				ClientKeyData:         cfg.KeyData,
				Token:                 cfg.BearerToken,
			},
			"bogus": {Token: "nope"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"envtest": {Cluster: "envtest", AuthInfo: "envtest", Namespace: "default"},
			"bogus":   {Cluster: "bogus", AuthInfo: "bogus"},
		},
		CurrentContext: "envtest",
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(kc, path))
	return path
}

func get(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, rdr))
	return rec
}

func TestContextEndpointsAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	handler := server.New(server.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   kube.NewManager(writeTwoContextKubeconfig(t, cfg)),
		Dist:   os.DirFS(t.TempDir()),
	})

	t.Run("contexts enumerates both, envtest active", func(t *testing.T) {
		rec := get(t, handler, http.MethodGet, "/api/v1/contexts", "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []kube.ContextInfo `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 2)
		active := map[string]bool{}
		for _, c := range body.Items {
			active[c.Name] = c.Active
		}
		assert.True(t, active["envtest"], "envtest is the active context")
		assert.False(t, active["bogus"])
	})

	t.Run("overview summarizes the live cluster", func(t *testing.T) {
		rec := get(t, handler, http.MethodGet, "/api/v1/overview", "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body resources.OverviewResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "envtest", body.Context)
		assert.NotEmpty(t, body.ServerVersion, "server version reported")
		assert.Contains(t, body.Namespaces, "default")
		assert.Contains(t, body.Namespaces, "kube-system")
	})

	t.Run("health probes concurrently without one context blocking the other", func(t *testing.T) {
		start := time.Now()
		rec := get(t, handler, http.MethodGet, "/api/v1/contexts/health", "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		require.Less(t, time.Since(start), 20*time.Second, "unreachable context must not stall the probe")

		var body struct {
			Items []kube.ContextHealth `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		byName := map[string]kube.ContextHealth{}
		for _, h := range body.Items {
			byName[h.Name] = h
		}
		assert.True(t, byName["envtest"].Reachable)
		assert.True(t, byName["envtest"].AuthOK)
		assert.NotEmpty(t, byName["envtest"].ServerVersion)
		assert.False(t, byName["bogus"].Reachable, "127.0.0.1:1 is unreachable")
		assert.NotEmpty(t, byName["bogus"].Error, "failure reason is surfaced")
	})

	t.Run("switch retargets subsequent calls", func(t *testing.T) {
		rec := get(t, handler, http.MethodPost, "/api/v1/contexts/switch", `{"name":"bogus"}`)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		// Overview now targets the unreachable cluster: a clean error, not a hang.
		rec = get(t, handler, http.MethodGet, "/api/v1/overview", "")
		assert.Equal(t, http.StatusBadGateway, rec.Code, "body: %s", rec.Body.String())

		// Switch back and confirm the live cluster is served again.
		rec = get(t, handler, http.MethodPost, "/api/v1/contexts/switch", `{"name":"envtest"}`)
		require.Equal(t, http.StatusOK, rec.Code)
		rec = get(t, handler, http.MethodGet, "/api/v1/overview", "")
		assert.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	})
}
