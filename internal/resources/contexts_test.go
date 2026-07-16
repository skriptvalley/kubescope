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
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
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
	dynamic      dynamic.Interface
	dynamicErr   error
	discovery    discovery.DiscoveryInterface
	discoveryErr error
	// FB-6 additions. execCmd/loopback are the classification hints
	// ClassifyActiveError feeds the real kube.ClassifyError, so handler tests
	// exercise the actual taxonomy. kubeconfigPath/probeHealth/setErr drive the
	// setup-state and set-kubeconfig handlers; setPath captures the last set.
	execCmd        string
	loopback       bool
	kubeconfigPath string
	probeHealth    kube.ContextHealth
	setErr         error
	setPath        string
	sourceGen      int64
}

func (f *fakeCluster) Clientset() (kubernetes.Interface, error) { return f.clientset, f.clientsetErr }
func (f *fakeCluster) ActiveContextName() (string, error)       { return f.active, f.activeErr }
func (f *fakeCluster) Contexts() ([]kube.ContextInfo, error)    { return f.contexts, f.contextsErr }
func (f *fakeCluster) SwitchContext(name string) error          { f.switched = name; return f.switchErr }
func (f *fakeCluster) ExecGuidance(string) string               { return f.execGuidance }
func (f *fakeCluster) Dynamic() (dynamic.Interface, error)      { return f.dynamic, f.dynamicErr }
func (f *fakeCluster) DiscoveryFor(string) (discovery.DiscoveryInterface, error) {
	return f.discovery, f.discoveryErr
}
func (f *fakeCluster) ProbeAll(context.Context) ([]kube.ContextHealth, error) {
	return f.health, f.healthErr
}
func (f *fakeCluster) KubeconfigPath() string { return f.kubeconfigPath }
func (f *fakeCluster) SetKubeconfigPath(path string) error {
	f.setPath = path
	if f.setErr != nil {
		return f.setErr
	}
	f.kubeconfigPath = path
	return nil
}
func (f *fakeCluster) ProbeContext(context.Context, string) kube.ContextHealth { return f.probeHealth }
func (f *fakeCluster) SourceGeneration() int64                                 { return f.sourceGen }
func (f *fakeCluster) ClassifyActiveError(err error) kube.Classification {
	return kube.ClassifyError(err, kube.ClassifyHints{ExecCommand: f.execCmd, LoopbackServer: f.loopback})
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
		HealthHandler(cluster, discardLogger(), nil)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/contexts/health", nil))

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
		HealthHandler(cluster, discardLogger(), nil)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/contexts/health", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})
}

// TestHealthHandlerNotifiesObserver pins that every probe result reaches the
// observer (the server wires it to the stream hub, FB-6 Story D).
func TestHealthHandlerNotifiesObserver(t *testing.T) {
	cluster := &fakeCluster{health: []kube.ContextHealth{
		{Name: "a", Reachable: true, AuthOK: true},
		{Name: "b", Reason: "connection_refused", Error: "refused"},
	}}
	var seen []string
	rec := httptest.NewRecorder()
	HealthHandler(cluster, discardLogger(), func(h kube.ContextHealth) { seen = append(seen, h.Name) })(
		rec, httptest.NewRequest(http.MethodGet, "/api/v1/contexts/health", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"a", "b"}, seen)
}

// TestHealthHandlerSkipsObserverOnCanceledRequest pins the PR-review fix: a
// probe run under an already-canceled request context says nothing about the
// cluster, so it must never be synced into the watch layer as an outage.
func TestHealthHandlerSkipsObserverOnCanceledRequest(t *testing.T) {
	cluster := &fakeCluster{health: []kube.ContextHealth{{Name: "a", Error: "context canceled"}}}
	notified := 0
	req := httptest.NewRequest(http.MethodGet, "/api/v1/contexts/health", nil)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	rec := httptest.NewRecorder()
	HealthHandler(cluster, discardLogger(), func(kube.ContextHealth) { notified++ })(rec, req.WithContext(ctx))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Zero(t, notified, "a canceled request's probe results must not reach the observer")
}
