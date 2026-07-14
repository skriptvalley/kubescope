package resources

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestParseGVR(t *testing.T) {
	tests := []struct {
		name                     string
		group, version, resource string
		want                     schema.GroupVersionResource
	}{
		{"core token maps to empty group", "core", "v1", "pods",
			schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}},
		{"named group passes through", "apps", "v1", "deployments",
			schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
		{"CRD group passes through", "example.com", "v1", "widgets",
			schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseGVR(tt.group, tt.version, tt.resource))
		})
	}
}

func TestShapeList(t *testing.T) {
	t.Run("namespaced kind carries a namespace column and per-row fields", func(t *testing.T) {
		info := APIResourceInfo{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true}
		list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{
				"name": "d1", "namespace": "default", "uid": "u1",
				"creationTimestamp": "2026-07-14T10:00:00Z",
			}}},
		}}
		got := shapeList(info, list)

		assert.True(t, got.Namespaced)
		assert.Equal(t, []listColumn{{ID: "name", Header: "Name"}, {ID: "namespace", Header: "Namespace"}, {ID: "age", Header: "Age"}}, got.Columns)
		require.Len(t, got.Rows, 1)
		assert.Equal(t, listRow{Name: "d1", Namespace: "default", CreationTimestamp: "2026-07-14T10:00:00Z", UID: "u1"}, got.Rows[0])
	})

	t.Run("cluster-scoped kind omits the namespace column", func(t *testing.T) {
		info := APIResourceInfo{Version: "v1", Resource: "nodes", Kind: "Node", Namespaced: false}
		list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "n1"}}},
		}}
		got := shapeList(info, list)

		assert.False(t, got.Namespaced)
		assert.Equal(t, []listColumn{{ID: "name", Header: "Name"}, {ID: "age", Header: "Age"}}, got.Columns)
		require.Len(t, got.Rows, 1)
		assert.Equal(t, "n1", got.Rows[0].Name)
		assert.Empty(t, got.Rows[0].Namespace, "no namespace for a cluster-scoped object")
		assert.Empty(t, got.Rows[0].CreationTimestamp, "a missing timestamp is omitted, not zero-valued")
	})

	t.Run("empty list serializes rows as an array, not null", func(t *testing.T) {
		got := shapeList(APIResourceInfo{Resource: "pods", Namespaced: true}, &unstructured.UnstructuredList{})
		out, err := json.Marshal(got)
		require.NoError(t, err)
		assert.Contains(t, string(out), `"rows":[]`)
	})
}

// widgetGVR is a namespaced CRD used across the generic handler tests.
var widgetGVR = schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}

func unstructuredWidget(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata": map[string]any{
			"name":              name,
			"namespace":         namespace,
			"uid":               "uid-" + name,
			"creationTimestamp": "2026-07-14T10:00:00Z",
		},
	}}
}

// genericTestServer wires the generic routes on a real chi router so URL params
// are populated exactly as in production, backed by a fake dynamic client and a
// discovery service that knows a namespaced CRD (widgets) and a cluster-scoped
// core kind (nodes). It returns the fake dynamic client so tests can inject
// reactors (e.g. a Forbidden error).
func genericTestServer(t *testing.T, objects ...*unstructured.Unstructured) (http.Handler, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	objs := make([]runtime.Object, 0, len(objects))
	for _, o := range objects {
		objs = append(objs, o)
	}
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{widgetGVR: "WidgetList"}, objs...)

	disc := &countingDiscovery{lists: []*metav1.APIResourceList{
		apiList("v1", apiRes("nodes", "Node", false, "get", "list")),
		apiList("example.com/v1", apiRes("widgets", "Widget", true, "get", "list", "watch")),
	}}
	cluster := &fakeCluster{active: "ctx-a", dynamic: dc, discovery: disc}
	svc := NewDiscoveryService(cluster)

	r := chi.NewRouter()
	r.Get("/resources/{group}/{version}/{resource}", ListHandler(cluster, svc, discardLogger()))
	r.Get("/resources/{group}/{version}/{resource}/{name}", GetHandler(cluster, svc, discardLogger()))
	r.Get("/resources/{group}/{version}/{resource}/{name}/yaml", YAMLHandler(cluster, svc, discardLogger()))
	return r, dc
}

func TestGenericHandlers(t *testing.T) {
	srv, _ := genericTestServer(t, unstructuredWidget("w1", "default"), unstructuredWidget("w2", "kube-system"))

	do := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	t.Run("list across all namespaces", func(t *testing.T) {
		rec := do("/resources/example.com/v1/widgets")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body listResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.True(t, body.Namespaced)
		assert.Len(t, body.Rows, 2, "both namespaces listed when no namespace given")
	})

	t.Run("list a single namespace", func(t *testing.T) {
		rec := do("/resources/example.com/v1/widgets?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body listResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Rows, 1)
		assert.Equal(t, "w1", body.Rows[0].Name)
	})

	t.Run("namespace on a cluster-scoped resource is a 400", func(t *testing.T) {
		rec := do("/resources/core/v1/nodes?namespace=default")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_scope", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("unknown GVR is a structured 404", func(t *testing.T) {
		rec := do("/resources/example.com/v1/gadgets")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "unknown_resource", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("get returns the full object", func(t *testing.T) {
		rec := do("/resources/example.com/v1/widgets/w1?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body objectResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		meta, ok := body.Object["metadata"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "w1", meta["name"])
	})

	t.Run("get a namespaced object without a namespace is a 400", func(t *testing.T) {
		rec := do("/resources/example.com/v1/widgets/w1")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_scope", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("get a missing object is a structured 404", func(t *testing.T) {
		rec := do("/resources/example.com/v1/widgets/ghost?namespace=default")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "not_found", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("yaml renders the object", func(t *testing.T) {
		rec := do("/resources/example.com/v1/widgets/w1/yaml?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body yamlResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Contains(t, body.YAML, "kind: Widget")
		assert.Contains(t, body.YAML, "name: w1")
	})
}

func TestGenericHandlerForbidden(t *testing.T) {
	srv, dc := genericTestServer(t, unstructuredWidget("w1", "default"))
	// RBAC-restricted read: the apiserver returns Forbidden for the Get.
	dc.PrependReactor("get", "widgets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "example.com", Resource: "widgets"}, "w1", errors.New("access denied"))
	})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/resources/example.com/v1/widgets/w1?namespace=default", nil))

	assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, "forbidden", errorCode(t, rec.Body.Bytes()))
}
