package resources_test

// Envtest-backed integration test for the Sprint 7 config/networking/storage
// engine: boots a real kube-apiserver, seeds a Secret, a Service+Endpoints and a
// few named objects, then drives the masked Secret reads + reveal, the typed
// Service endpoints resolution, and the global search through the full chain
// kubeconfig → kube.Manager → router (ADR-0003, ADR-0005).
//
// Requires KUBEBUILDER_ASSETS (make test sets it); skipped otherwise.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/resources"
	"github.com/skriptvalley/kubescope/internal/server"
)

func TestSprint7EngineAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	ns := "default"

	// Secret with a real value — the reveal path must decode it, the masked reads
	// must never expose it. The last-applied-configuration annotation embeds the
	// plaintext (as `kubectl apply` would write it), so masking must redact the
	// annotation too, not just the data field.
	const secretValue = "s3cr3t-value"
	_, err = client.CoreV1().Secrets(ns).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-creds",
			Namespace: ns,
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret","stringData":{"password":"` + secretValue + `"}}`,
			},
		},
		Data: map[string][]byte{"password": []byte(secretValue)},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Service + Endpoints (envtest has no endpoints controller, so materialise the
	// Endpoints ourselves) to exercise typed endpoints resolution.
	_, err = client.CoreV1().Services(ns).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: ns},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "web"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = client.CoreV1().Endpoints(ns).Create(ctx, &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: ns},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{
				{IP: "10.1.0.1", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: ns, Name: "web-1"}},
			},
			NotReadyAddresses: []corev1.EndpointAddress{
				{IP: "10.1.0.2", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: ns, Name: "web-2"}},
			},
		}},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	handler := server.New(server.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   kube.NewManager(writeKubeconfig(t, cfg)),
		Dist:   os.DirFS(t.TempDir()),
	})

	do := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	t.Run("secret get masks data by default", func(t *testing.T) {
		rec := do("/api/v1/resources/core/v1/secrets/db-creds?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.NotContains(t, rec.Body.String(), secretValue, "the raw secret value must not travel on a masked get")
		var body struct {
			Object map[string]any `json:"object"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		data, _ := body.Object["data"].(map[string]any)
		require.Contains(t, data, "password", "the key is preserved")
		assert.NotEqual(t, secretValue, data["password"], "the value is redacted")
	})

	t.Run("secret yaml masks data by default", func(t *testing.T) {
		rec := do("/api/v1/resources/core/v1/secrets/db-creds/yaml?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.NotContains(t, rec.Body.String(), secretValue)
		// Base64 of the value must not leak either.
		assert.NotContains(t, rec.Body.String(), "czNjcjN0LXZhbHVl")
	})

	t.Run("reveal decodes exactly the requested key", func(t *testing.T) {
		rec := do("/api/v1/secrets/default/db-creds/reveal?key=password")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct{ Key, Value string }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "password", body.Key)
		assert.Equal(t, secretValue, body.Value, "reveal returns the decoded plaintext")
	})

	t.Run("service detail resolves ready and not-ready endpoints", func(t *testing.T) {
		rec := do("/api/v1/services/default/web")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var detail resources.ServiceDetail
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
		assert.Equal(t, "ClusterIP", detail.Type)
		assert.True(t, detail.EndpointsFound)
		require.Len(t, detail.ReadyAddresses, 1)
		assert.Equal(t, "web-1", detail.ReadyAddresses[0].TargetRef.Name)
		require.Len(t, detail.NotReadyAddresses, 1)
		assert.Equal(t, "web-2", detail.NotReadyAddresses[0].TargetRef.Name)
	})

	t.Run("service list carries enriched columns", func(t *testing.T) {
		rec := do("/api/v1/resources/core/v1/services?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Columns []struct{ ID string } `json:"columns"`
			Rows    []struct {
				Name  string            `json:"name"`
				Cells map[string]string `json:"cells"`
			} `json:"rows"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		ids := map[string]bool{}
		for _, c := range body.Columns {
			ids[c.ID] = true
		}
		assert.True(t, ids["type"] && ids["cluster-ip"] && ids["ports"] && ids["selector"], "service columns are enriched")
		for _, row := range body.Rows {
			if row.Name == "web" {
				assert.Equal(t, "app=web", row.Cells["selector"])
				assert.Equal(t, "80/TCP", row.Cells["ports"])
			}
		}
	})

	t.Run("search matches by name across discovered types", func(t *testing.T) {
		rec := do("/api/v1/search?q=db-creds")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body resources.SearchResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		found := false
		for _, r := range body.Results {
			if r.Name == "db-creds" && r.Resource == "secrets" {
				found = true
				assert.Equal(t, "default", r.Namespace)
				assert.True(t, r.Namespaced)
			}
		}
		assert.True(t, found, "the secret is found by name")
		// Search must not leak the secret value even though it lists secrets.
		assert.NotContains(t, rec.Body.String(), secretValue)
	})

	t.Run("search rejects an empty query", func(t *testing.T) {
		rec := do("/api/v1/search?q=")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("search bounds the result set", func(t *testing.T) {
		// Seed several matching configmaps, then cap the limit at 2.
		for _, n := range []string{"searchme-1", "searchme-2", "searchme-3"} {
			_, err := client.CoreV1().ConfigMaps(ns).Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns},
			}, metav1.CreateOptions{})
			require.NoError(t, err)
		}
		rec := do("/api/v1/search?q=searchme&limit=2")
		require.Equal(t, http.StatusOK, rec.Code)
		var body resources.SearchResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Len(t, body.Results, 2)
		assert.True(t, body.Truncated, "more matches than the limit flags truncation")
		for _, r := range body.Results {
			assert.True(t, strings.HasPrefix(r.Name, "searchme"))
		}
	})
}
