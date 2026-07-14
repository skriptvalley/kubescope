package resources

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNamespacesHandler(t *testing.T) {
	t.Run("returns sorted namespace names", func(t *testing.T) {
		cs := fake.NewClientset(namespace("kube-system"), namespace("default"), namespace("apps"))
		cluster := &fakeCluster{active: "ctx-a", clientset: cs}
		rec := httptest.NewRecorder()
		NamespacesHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil))

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body namespaceList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, []string{"apps", "default", "kube-system"}, body.Items)
	})

	t.Run("empty cluster returns an array, not null", func(t *testing.T) {
		cluster := &fakeCluster{active: "ctx-a", clientset: fake.NewClientset()}
		rec := httptest.NewRecorder()
		NamespacesHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"items":[]`)
	})

	t.Run("kubeconfig unavailable is a structured 503", func(t *testing.T) {
		cluster := &fakeCluster{clientsetErr: errors.New("loading kubeconfig: boom")}
		rec := httptest.NewRecorder()
		NamespacesHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("unreachable cluster is a structured 502", func(t *testing.T) {
		cs := fake.NewClientset()
		cs.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("connection refused")
		})
		cluster := &fakeCluster{active: "ctx-a", clientset: cs}
		rec := httptest.NewRecorder()
		NamespacesHandler(cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/namespaces", nil))

		assert.Equal(t, http.StatusBadGateway, rec.Code)
		assert.Equal(t, "cluster_unreachable", errorCode(t, rec.Body.Bytes()))
	})
}
