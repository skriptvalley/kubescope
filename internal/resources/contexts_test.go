package resources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// fakeCluster implements resources.Cluster for handler tests.
type fakeCluster struct {
	clientset    kubernetes.Interface
	clientsetErr error
	active       string
	activeErr    error
	contexts     []kube.ContextInfo
	contextsErr  error
	switchErr    error
	switched     string
	health       []kube.ContextHealth
	healthErr    error
	execGuidance string
}

func (f *fakeCluster) Clientset() (kubernetes.Interface, error) { return f.clientset, f.clientsetErr }
func (f *fakeCluster) ActiveContextName() (string, error)       { return f.active, f.activeErr }
func (f *fakeCluster) Contexts() ([]kube.ContextInfo, error)    { return f.contexts, f.contextsErr }
func (f *fakeCluster) SwitchContext(name string) error          { f.switched = name; return f.switchErr }
func (f *fakeCluster) ExecGuidance(string) string               { return f.execGuidance }
func (f *fakeCluster) ProbeAll(context.Context) ([]kube.ContextHealth, error) {
	return f.health, f.healthErr
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Error.Code
}

func TestContextsHandler(t *testing.T) {
	t.Run("lists contexts", func(t *testing.T) {
		cluster := &fakeCluster{contexts: []kube.ContextInfo{
			{Name: "a", Cluster: "ca", Namespace: "default", Active: true},
			{Name: "b", Cluster: "cb", Namespace: "kube-system", Active: false},
		}}
		rec := httptest.NewRecorder()
		ContextsHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/contexts", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Items []kube.ContextInfo `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 2)
		assert.True(t, body.Items[0].Active)
	})

	t.Run("kubeconfig error is structured 503", func(t *testing.T) {
		cluster := &fakeCluster{contextsErr: errors.New("loading kubeconfig: boom")}
		rec := httptest.NewRecorder()
		ContextsHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/contexts", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})
}

func TestSwitchContextHandler(t *testing.T) {
	t.Run("switches and returns refreshed list", func(t *testing.T) {
		cluster := &fakeCluster{contexts: []kube.ContextInfo{{Name: "b", Active: true}}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contexts/switch", strings.NewReader(`{"name":"b"}`))
		SwitchContextHandler(cluster, discardLogger())(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Equal(t, "b", cluster.switched)
		var body struct {
			Items []kube.ContextInfo `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 1)
		assert.True(t, body.Items[0].Active)
	})

	t.Run("invalid JSON is 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contexts/switch", strings.NewReader(`not json`))
		SwitchContextHandler(&fakeCluster{}, discardLogger())(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("empty name is 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contexts/switch", strings.NewReader(`{"name":""}`))
		SwitchContextHandler(&fakeCluster{}, discardLogger())(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("unknown context is 404", func(t *testing.T) {
		cluster := &fakeCluster{switchErr: &kube.UnknownContextError{Name: "ghost"}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contexts/switch", strings.NewReader(`{"name":"ghost"}`))
		SwitchContextHandler(cluster, discardLogger())(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "unknown_context", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("trailing data after the object is rejected", func(t *testing.T) {
		cluster := &fakeCluster{contexts: []kube.ContextInfo{{Name: "a", Active: true}}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contexts/switch",
			strings.NewReader(`{"name":"a"}{"name":"b"}`))
		SwitchContextHandler(cluster, discardLogger())(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.switched, "no switch performed on a malformed body")
	})

	t.Run("oversized body is rejected", func(t *testing.T) {
		cluster := &fakeCluster{}
		huge := `{"name":"` + strings.Repeat("A", (64<<10)+1) + `"}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contexts/switch", strings.NewReader(huge))
		SwitchContextHandler(cluster, discardLogger())(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.switched)
	})
}

func TestHealthHandler(t *testing.T) {
	t.Run("returns per-context health", func(t *testing.T) {
		cluster := &fakeCluster{health: []kube.ContextHealth{
			{Name: "a", Reachable: true, AuthOK: true, ServerVersion: "v1.33.0"},
			{Name: "b", Reachable: false, Error: "connection refused"},
		}}
		rec := httptest.NewRecorder()
		HealthHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/contexts/health", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Items []kube.ContextHealth `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 2)
		assert.Equal(t, "v1.33.0", body.Items[0].ServerVersion)
		assert.False(t, body.Items[1].Reachable)
	})

	t.Run("kubeconfig error is structured 503", func(t *testing.T) {
		cluster := &fakeCluster{healthErr: errors.New("loading kubeconfig: boom")}
		rec := httptest.NewRecorder()
		HealthHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/contexts/health", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})
}
