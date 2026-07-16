package resources

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func namespace(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func clusterWithVersion(t *testing.T, gitVersion string, objs ...runtime.Object) kubernetes.Interface {
	t.Helper()
	cs := fake.NewClientset(objs...)
	disco, ok := cs.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(t, ok)
	disco.FakedServerVersion = &version.Info{GitVersion: gitVersion}
	return cs
}

func TestOverviewHandler(t *testing.T) {
	t.Run("summarizes the active cluster", func(t *testing.T) {
		cs := clusterWithVersion(t, "v1.33.0",
			node("n1", corev1.ConditionTrue, "v1.33.0"),
			node("n2", corev1.ConditionTrue, "v1.33.0"),
			namespace("kube-system"),
			namespace("default"),
		)
		cluster := &fakeCluster{active: "prod", clientset: cs}
		rec := httptest.NewRecorder()
		OverviewHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body OverviewResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "prod", body.Context)
		assert.Equal(t, "v1.33.0", body.ServerVersion)
		assert.Equal(t, 2, body.NodeCount)
		assert.Equal(t, []string{"default", "kube-system"}, body.Namespaces, "namespaces sorted")
	})

	t.Run("no active context is structured 503", func(t *testing.T) {
		cluster := &fakeCluster{activeErr: errors.New("kubeconfig has no current-context set")}
		rec := httptest.NewRecorder()
		OverviewHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("kubeconfig unavailable is structured 503", func(t *testing.T) {
		cluster := &fakeCluster{active: "prod", clientsetErr: errors.New("loading kubeconfig: boom")}
		rec := httptest.NewRecorder()
		OverviewHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("connection refused is classified 502 connection_refused", func(t *testing.T) {
		cs := clusterWithVersion(t, "v1.33.0")
		cs.(*fake.Clientset).PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("connection refused")
		})
		cluster := &fakeCluster{active: "prod", clientset: cs}
		rec := httptest.NewRecorder()
		OverviewHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))

		assert.Equal(t, http.StatusBadGateway, rec.Code)
		assert.Equal(t, "connection_refused", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("exec-plugin failure carries ADR-0004 guidance", func(t *testing.T) {
		cs := clusterWithVersion(t, "v1.33.0")
		cs.(*fake.Clientset).PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New(`exec: "aws": executable file not found in $PATH`)
		})
		cluster := &fakeCluster{active: "eks", clientset: cs, execCmd: "aws"}
		rec := httptest.NewRecorder()
		OverviewHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))

		require.Equal(t, http.StatusBadGateway, rec.Code)
		var env struct {
			Error struct {
				Code     string `json:"code"`
				Guidance string `json:"guidance"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		assert.Equal(t, "cluster_unreachable", env.Error.Code)
		assert.Contains(t, env.Error.Guidance, "ADR-0004", "overview surfaces exec guidance like the health probe does")
	})
}
