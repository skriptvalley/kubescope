package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// chiRequest builds a request with chi URL params injected directly, so a
// handler that reads chi.URLParam can be exercised without standing up a router.
func chiRequest(method, target, body string, params map[string]string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		wantN   string
	}{
		{name: "valid object", yaml: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm1\n", wantN: "cm1"},
		{name: "empty document", yaml: "", wantErr: true},
		{name: "whitespace only", yaml: "\n  \n", wantErr: true},
		{name: "bare scalar is not an object", yaml: "just-a-string", wantErr: true},
		{name: "list is not an object", yaml: "- a\n- b\n", wantErr: true},
		{name: "malformed yaml", yaml: "a: : b", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := parseManifest(tt.yaml)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantN, obj.GetName())
		})
	}
}

func TestRestartPatchShape(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC)
	patch := restartPatch(now)

	var m map[string]any
	require.NoError(t, json.Unmarshal(patch, &m), "patch must be valid JSON")

	// spec.template.metadata.annotations[restartedAtAnnotation] == RFC3339 now.
	spec := m["spec"].(map[string]any)
	tmpl := spec["template"].(map[string]any)
	meta := tmpl["metadata"].(map[string]any)
	annos := meta["annotations"].(map[string]any)
	assert.Equal(t, "2026-07-15T10:30:00Z", annos[restartedAtAnnotation])
	assert.Len(t, annos, 1, "patch touches only the restart annotation")
}

func TestRestartable(t *testing.T) {
	for resource, want := range map[string]bool{
		resourceDeployments:  true,
		resourceStatefulSets: true,
		resourceDaemonSets:   true,
		resourceReplicaSets:  false,
		resourcePods:         false,
		resourceJobs:         false,
	} {
		assert.Equal(t, want, restartable(resource), "restartable(%q)", resource)
	}
}

func TestScaleHandlerValidation(t *testing.T) {
	cluster := &fakeCluster{clientset: fake.NewClientset()}
	h := ScaleHandler(cluster, discardLogger())

	tests := []struct {
		name     string
		resource string
		body     string
		wantCode string
	}{
		{name: "malformed body", resource: "deployments", body: "not json", wantCode: "invalid_request"},
		{name: "missing replicas", resource: "deployments", body: `{}`, wantCode: "invalid_request"},
		{name: "negative replicas", resource: "deployments", body: `{"replicas":-1}`, wantCode: "invalid_request"},
		{name: "unscalable kind", resource: "pods", body: `{"replicas":2}`, wantCode: "unscalable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h(rec, chiRequest(http.MethodPost, "/", tt.body,
				map[string]string{"resource": tt.resource, "namespace": "default", "name": "x"}))
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, tt.wantCode, errorCode(t, rec.Body.Bytes()))
		})
	}
}

func TestRestartHandlerRejectsUnrestartable(t *testing.T) {
	cluster := &fakeCluster{clientset: fake.NewClientset()}
	h := RestartHandler(cluster, discardLogger())
	rec := httptest.NewRecorder()
	h(rec, chiRequest(http.MethodPost, "/", "",
		map[string]string{"resource": "replicasets", "namespace": "default", "name": "x"}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "unrestartable", errorCode(t, rec.Body.Bytes()))
}
