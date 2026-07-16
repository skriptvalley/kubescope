package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// fakeProvider satisfies resources.Cluster for router-level tests.
type fakeProvider struct {
	clientset kubernetes.Interface
	err       error
}

func (f *fakeProvider) Clientset() (kubernetes.Interface, error) { return f.clientset, f.err }

func (f *fakeProvider) ActiveContextName() (string, error) { return "test", f.err }

func (f *fakeProvider) Contexts() ([]kube.ContextInfo, error) {
	return []kube.ContextInfo{{Name: "test", Cluster: "test", Namespace: "default", Active: true}}, f.err
}

func (f *fakeProvider) SwitchContext(string) error { return f.err }

func (f *fakeProvider) ProbeAll(context.Context) ([]kube.ContextHealth, error) {
	return []kube.ContextHealth{{Name: "test", Reachable: true, AuthOK: true, ServerVersion: "v1.33.0"}}, f.err
}

func (f *fakeProvider) ExecGuidance(string) string { return "" }

func (f *fakeProvider) Dynamic() (dynamic.Interface, error) { return nil, f.err }

func (f *fakeProvider) DiscoveryFor(string) (discovery.DiscoveryInterface, error) {
	if f.clientset == nil {
		return nil, f.err
	}
	return f.clientset.Discovery(), f.err
}

func (f *fakeProvider) KubeconfigPath() string { return "/kubeconfig" }

func (f *fakeProvider) SetKubeconfigPath(string) error { return f.err }

func (f *fakeProvider) ProbeContext(context.Context, string) kube.ContextHealth {
	return kube.ContextHealth{Name: "test", Reachable: true, AuthOK: true, ServerVersion: "v1.33.0"}
}

func (f *fakeProvider) ClassifyActiveError(err error) kube.Classification {
	return kube.ClassifyError(err, kube.ClassifyHints{})
}

// ClientsetFor / RestConfigFor let fakeProvider satisfy stream.ExecCluster and
// stream.PortForwardCluster so the exec + port-forward routes register in tests.
func (f *fakeProvider) ClientsetFor(string) (kubernetes.Interface, error) {
	return f.clientset, f.err
}

func (f *fakeProvider) RestConfigFor(string) (*rest.Config, error) {
	return &rest.Config{Host: "https://example.test"}, f.err
}

func testServer(t *testing.T, dist fstest.MapFS) http.Handler {
	t.Helper()
	return New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   &fakeProvider{clientset: fake.NewClientset()},
		Dist:   dist,
	})
}

func spaFixture() fstest.MapFS {
	return fstest.MapFS{
		"index.html":          &fstest.MapFile{Data: []byte("<html>kubescope spa</html>")},
		"assets/app-abc12.js": &fstest.MapFile{Data: []byte("console.log('app')")},
	}
}

func TestRouting(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		wantStatus     int
		wantBody       string
		wantJSONErrTag string
	}{
		{
			name:       "healthz",
			path:       "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "root serves index",
			path:       "/",
			wantStatus: http.StatusOK,
			wantBody:   "kubescope spa",
		},
		{
			name:       "existing asset served as-is",
			path:       "/assets/app-abc12.js",
			wantStatus: http.StatusOK,
			wantBody:   "console.log('app')",
		},
		{
			name:       "unknown path falls back to index",
			path:       "/nodes",
			wantStatus: http.StatusOK,
			wantBody:   "kubescope spa",
		},
		{
			name:       "deep unknown path falls back to index",
			path:       "/some/client/route",
			wantStatus: http.StatusOK,
			wantBody:   "kubescope spa",
		},
		{
			name:           "unknown api path is json 404, not spa",
			path:           "/api/v1/does-not-exist",
			wantStatus:     http.StatusNotFound,
			wantJSONErrTag: "not_found",
		},
		{
			name:           "api root is json 404, not spa",
			path:           "/api",
			wantStatus:     http.StatusNotFound,
			wantJSONErrTag: "not_found",
		},
		{
			name:       "known api route works",
			path:       "/api/v1/nodes",
			wantStatus: http.StatusOK,
			wantBody:   `{"items":[]}`,
		},
		{
			name:           "wrong method on api route is json 405, not empty body",
			method:         http.MethodPost,
			path:           "/api/v1/nodes",
			wantStatus:     http.StatusMethodNotAllowed,
			wantJSONErrTag: "method_not_allowed",
		},
		{
			name:           "get on post-only switch route is json 405",
			method:         http.MethodGet,
			path:           "/api/v1/contexts/switch",
			wantStatus:     http.StatusMethodNotAllowed,
			wantJSONErrTag: "method_not_allowed",
		},
	}

	srv := testServer(t, spaFixture())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(method, tt.path, nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.Contains(t, rec.Body.String(), tt.wantBody)
			}
			if tt.wantJSONErrTag != "" {
				var envelope struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
				assert.Equal(t, tt.wantJSONErrTag, envelope.Error.Code)
			}
		})
	}
}

func TestSPAIndexNotCached(t *testing.T) {
	srv := testServer(t, spaFixture())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nodes", nil))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
}

func TestSPAWithoutEmbeddedBuild(t *testing.T) {
	srv := testServer(t, fstest.MapFS{".gitkeep": &fstest.MapFile{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "make build")
}
