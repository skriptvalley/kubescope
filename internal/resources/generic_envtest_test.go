package resources_test

// Envtest-backed integration test for the Sprint 2 generic resource engine:
// boots a real kube-apiserver, installs a sample CRD, seeds custom + core
// objects, and drives discovery, generic list/get/yaml and namespace scoping
// through the full chain kubeconfig → kube.Manager → router (ADR-0003).
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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/resources"
	"github.com/skriptvalley/kubescope/internal/server"
)

func boolPtr(b bool) *bool { return &b }

func widgetCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "widgets",
				Singular: "widget",
				Kind:     "Widget",
				ListKind: "WidgetList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: boolPtr(true),
					},
				},
			}},
		},
	}
}

func widget(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"spec":       map[string]any{"size": "large"},
	}}
}

func TestGenericEngineAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{CRDs: []*apiextensionsv1.CustomResourceDefinition{widgetCRD()}}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	// Seed custom resources in two namespaces to exercise scoping.
	dyn, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)
	widgetGVR := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
	ctx := context.Background()
	_, err = dyn.Resource(widgetGVR).Namespace("default").Create(ctx, widget("w1", "default"), metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = dyn.Resource(widgetGVR).Namespace("kube-system").Create(ctx, widget("w2", "kube-system"), metav1.CreateOptions{})
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

	t.Run("discovery enumerates the installed CRD", func(t *testing.T) {
		// Discovery of a freshly-installed CRD can lag briefly; refresh bypasses
		// the cache each attempt so a stale first read never sticks.
		require.Eventually(t, func() bool {
			rec := do("/api/v1/discovery?refresh=true")
			if rec.Code != http.StatusOK {
				return false
			}
			var body resources.DiscoveryResult
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				return false
			}
			for _, g := range body.Groups {
				if g.Name != "example.com" {
					continue
				}
				for _, r := range g.Resources {
					if r.Resource == "widgets" && r.Kind == "Widget" && r.Namespaced {
						return true
					}
				}
			}
			return false
		}, 20*time.Second, 500*time.Millisecond, "the widgets CRD should appear in discovery")
	})

	t.Run("core group enumerated with the core token", func(t *testing.T) {
		rec := do("/api/v1/discovery")
		require.Equal(t, http.StatusOK, rec.Code)
		var body resources.DiscoveryResult
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.NotEmpty(t, body.Groups)
		assert.Equal(t, "", body.Groups[0].Name, "core group sorts first with the empty name")
	})

	t.Run("list a CRD in a single namespace", func(t *testing.T) {
		rec := do("/api/v1/resources/example.com/v1/widgets?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Namespaced bool `json:"namespaced"`
			Rows       []struct {
				Name, Namespace string
			} `json:"rows"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.True(t, body.Namespaced)
		require.Len(t, body.Rows, 1, "only the default-namespace widget")
		assert.Equal(t, "w1", body.Rows[0].Name)
		assert.Equal(t, "default", body.Rows[0].Namespace)
	})

	t.Run("list a CRD across all namespaces", func(t *testing.T) {
		rec := do("/api/v1/resources/example.com/v1/widgets")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Rows []struct{ Name string } `json:"rows"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		names := map[string]bool{}
		for _, r := range body.Rows {
			names[r.Name] = true
		}
		assert.True(t, names["w1"] && names["w2"], "both namespaces' widgets listed")
	})

	t.Run("get a single CRD object", func(t *testing.T) {
		rec := do("/api/v1/resources/example.com/v1/widgets/w1?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Object map[string]any `json:"object"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		meta, _ := body.Object["metadata"].(map[string]any)
		assert.Equal(t, "w1", meta["name"])
	})

	t.Run("yaml renders the CRD object", func(t *testing.T) {
		rec := do("/api/v1/resources/example.com/v1/widgets/w1/yaml?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			YAML string `json:"yaml"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Contains(t, body.YAML, "kind: Widget")
		assert.Contains(t, body.YAML, "name: w1")
	})

	t.Run("core namespaced kind lists via the core token", func(t *testing.T) {
		rec := do("/api/v1/resources/core/v1/configmaps?namespace=kube-system")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Namespaced bool `json:"namespaced"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.True(t, body.Namespaced)
	})

	t.Run("cluster-scoped kind rejects a namespace param", func(t *testing.T) {
		rec := do("/api/v1/resources/core/v1/namespaces?namespace=default")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("missing object is a structured 404", func(t *testing.T) {
		rec := do("/api/v1/resources/example.com/v1/widgets/ghost?namespace=default")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("namespace API lists namespaces", func(t *testing.T) {
		rec := do("/api/v1/namespaces")
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Items []string `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Contains(t, body.Items, "default")
		assert.Contains(t, body.Items, "kube-system")
	})
}
