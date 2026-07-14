package resources

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// countingDiscovery is a minimal discovery.DiscoveryInterface implementing only
// ServerPreferredResources (the one method the service calls), counting calls so
// cache and refresh behavior can be asserted. The embedded nil interface panics
// if any other method is invoked — a guard that the service stays that narrow.
type countingDiscovery struct {
	discovery.DiscoveryInterface
	lists []*metav1.APIResourceList
	err   error
	calls int
}

func (c *countingDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	c.calls++
	return c.lists, c.err
}

// fakeDiscoveryCluster is the narrow DiscoveryCluster the service depends on.
type fakeDiscoveryCluster struct {
	active    string
	activeErr error
	disc      discovery.DiscoveryInterface
	discErr   error
}

func (f *fakeDiscoveryCluster) ActiveContextName() (string, error) { return f.active, f.activeErr }
func (f *fakeDiscoveryCluster) Discovery() (discovery.DiscoveryInterface, error) {
	return f.disc, f.discErr
}

func apiList(gv string, resources ...metav1.APIResource) *metav1.APIResourceList {
	return &metav1.APIResourceList{GroupVersion: gv, APIResources: resources}
}

func apiRes(name, kind string, namespaced bool, verbs ...string) metav1.APIResource {
	return metav1.APIResource{Name: name, Kind: kind, Namespaced: namespaced, Verbs: verbs}
}

func TestShapeDiscovery(t *testing.T) {
	lists := []*metav1.APIResourceList{
		apiList("v1",
			apiRes("pods", "Pod", true, "get", "list", "watch"),
			apiRes("pods/status", "Pod", true, "get", "patch"), // subresource → dropped
			apiRes("nodes", "Node", false, "get", "list"),
			apiRes("bindings", "Binding", true, "create"), // not listable → dropped
		),
		apiList("apps/v1",
			apiRes("deployments", "Deployment", true, "get", "list", "watch"),
			apiRes("replicasets", "ReplicaSet", true, "get", "list"),
		),
	}

	res, err := shapeDiscovery(lists, nil)
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
	require.Len(t, res.Groups, 2)

	// Core group sorts first, listable non-subresources only, sorted by name.
	core := res.Groups[0]
	assert.Equal(t, "", core.Name)
	var coreNames []string
	for _, r := range core.Resources {
		coreNames = append(coreNames, r.Resource)
	}
	assert.Equal(t, []string{"nodes", "pods"}, coreNames)

	// GVR / scope / verbs carried through for the core entries.
	nodes := core.Resources[0]
	assert.Equal(t, APIResourceInfo{Group: "", Version: "v1", Resource: "nodes", Kind: "Node", Namespaced: false, Verbs: []string{"get", "list"}}, nodes)
	assert.True(t, core.Resources[1].Namespaced, "pods namespaced")

	apps := res.Groups[1]
	assert.Equal(t, "apps", apps.Name)
	assert.Equal(t, "apps", apps.Resources[0].Group)
	assert.Equal(t, "v1", apps.Resources[0].Version)
}

func TestShapeDiscoveryPartialFailure(t *testing.T) {
	gdf := &discovery.ErrGroupDiscoveryFailed{Groups: map[schema.GroupVersion]error{
		{Group: "metrics.k8s.io", Version: "v1beta1"}: errors.New("the server is currently unable to handle the request"),
	}}
	lists := []*metav1.APIResourceList{apiList("v1", apiRes("pods", "Pod", true, "list"))}

	res, err := shapeDiscovery(lists, gdf)
	require.NoError(t, err, "partial failure degrades gracefully, not fatal")
	require.Len(t, res.Groups, 1, "the reachable group is still returned")
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "metrics.k8s.io/v1beta1")
}

func TestShapeDiscoveryHardFailure(t *testing.T) {
	_, err := shapeDiscovery(nil, errors.New("connection refused"))
	require.Error(t, err, "a total discovery failure is surfaced, not swallowed")
}

func TestDiscoveryServiceCachesAndRefreshes(t *testing.T) {
	disc := &countingDiscovery{lists: []*metav1.APIResourceList{
		apiList("v1", apiRes("pods", "Pod", true, "list")),
	}}
	svc := NewDiscoveryService(&fakeDiscoveryCluster{active: "ctx-a", disc: disc})

	r1, err := svc.Get(false)
	require.NoError(t, err)
	require.Len(t, r1.Groups, 1)
	assert.Equal(t, 1, disc.calls)

	_, err = svc.Get(false)
	require.NoError(t, err)
	assert.Equal(t, 1, disc.calls, "second read is served from cache")

	// A CRD installed after startup: refresh must pick it up without a restart.
	disc.lists = append(disc.lists, apiList("apps/v1", apiRes("deployments", "Deployment", true, "list")))
	r3, err := svc.Get(true)
	require.NoError(t, err)
	assert.Equal(t, 2, disc.calls, "refresh re-fetches")
	require.Len(t, r3.Groups, 2, "the newly-served group appears after refresh")
}

func TestDiscoveryServiceCachesPerContext(t *testing.T) {
	disc := &countingDiscovery{lists: []*metav1.APIResourceList{
		apiList("v1", apiRes("pods", "Pod", true, "list")),
	}}
	cluster := &fakeDiscoveryCluster{active: "ctx-a", disc: disc}
	svc := NewDiscoveryService(cluster)

	_, _ = svc.Get(false)
	assert.Equal(t, 1, disc.calls)

	cluster.active = "ctx-b" // switching contexts is a cache miss
	_, _ = svc.Get(false)
	assert.Equal(t, 2, disc.calls)

	cluster.active = "ctx-a" // the first context is still cached
	_, _ = svc.Get(false)
	assert.Equal(t, 2, disc.calls)
}

func TestDiscoveryServiceKubeconfigError(t *testing.T) {
	svc := NewDiscoveryService(&fakeDiscoveryCluster{activeErr: errors.New("no current-context")})
	_, err := svc.Get(false)
	var kc *kubeconfigError
	require.ErrorAs(t, err, &kc, "kubeconfig problems are classified for a 503")
}

func TestDiscoveryHandler(t *testing.T) {
	newSvcCluster := func(disc discovery.DiscoveryInterface, active string, activeErr error) (*DiscoveryService, *fakeCluster) {
		cluster := &fakeCluster{active: active, activeErr: activeErr, discovery: disc}
		return NewDiscoveryService(cluster), cluster
	}

	t.Run("returns the shaped groups", func(t *testing.T) {
		disc := &countingDiscovery{lists: []*metav1.APIResourceList{
			apiList("v1", apiRes("pods", "Pod", true, "list")),
		}}
		svc, cluster := newSvcCluster(disc, "ctx-a", nil)
		rec := httptest.NewRecorder()
		DiscoveryHandler(svc, cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/discovery", nil))

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body DiscoveryResult
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Groups, 1)
	})

	t.Run("refresh query bypasses the cache", func(t *testing.T) {
		disc := &countingDiscovery{lists: []*metav1.APIResourceList{
			apiList("v1", apiRes("pods", "Pod", true, "list")),
		}}
		svc, cluster := newSvcCluster(disc, "ctx-a", nil)
		h := DiscoveryHandler(svc, cluster, discardLogger())

		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/discovery", nil))
		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/discovery", nil))
		assert.Equal(t, 1, disc.calls, "second plain read is cached")

		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/discovery?refresh=true", nil))
		assert.Equal(t, 2, disc.calls, "refresh re-fetches")
	})

	t.Run("kubeconfig error is a structured 503", func(t *testing.T) {
		svc, cluster := newSvcCluster(nil, "", errors.New("no current-context"))
		rec := httptest.NewRecorder()
		DiscoveryHandler(svc, cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/discovery", nil))

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("unreachable cluster is a structured 502", func(t *testing.T) {
		disc := &countingDiscovery{err: errors.New("connection refused")}
		svc, cluster := newSvcCluster(disc, "ctx-a", nil)
		rec := httptest.NewRecorder()
		DiscoveryHandler(svc, cluster, discardLogger())(rec, httptest.NewRequest(http.MethodGet, "/api/v1/discovery", nil))

		assert.Equal(t, http.StatusBadGateway, rec.Code)
		assert.Equal(t, "cluster_unreachable", errorCode(t, rec.Body.Bytes()))
	})
}
