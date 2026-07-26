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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/skriptvalley/kubescope/internal/graph"
)

var (
	errNoKubeconfig = errors.New("no kubeconfig source is usable")
	errUnreachable  = errors.New("dial tcp 127.0.0.1:6443: connect: connection refused")
)

// graphAPILists is the discovery snapshot the graph handler tests resolve
// against: the core kinds the builder reasons about, plus apps and batch.
var graphAPILists = []*metav1.APIResourceList{
	apiList("v1",
		apiRes("pods", "Pod", true, "get", "list"),
		apiRes("services", "Service", true, "get", "list"),
		apiRes("configmaps", "ConfigMap", true, "get", "list"),
		apiRes("secrets", "Secret", true, "get", "list"),
		apiRes("serviceaccounts", "ServiceAccount", true, "get", "list"),
		apiRes("endpoints", "Endpoints", true, "get", "list"),
		apiRes("nodes", "Node", false, "get", "list"),
	),
	apiList("apps/v1",
		apiRes("deployments", "Deployment", true, "get", "list"),
		apiRes("replicasets", "ReplicaSet", true, "get", "list"),
	),
	apiList("batch/v1", apiRes("jobs", "Job", true, "get", "list")),
}

func graphDiscovery() *DiscoveryService {
	return NewDiscoveryService(&fakeDiscoveryCluster{
		active: "ctx-a", disc: &countingDiscovery{lists: graphAPILists},
	})
}

func graphListKinds() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		{Version: "v1", Resource: "pods"}:                       "PodList",
		{Version: "v1", Resource: "services"}:                   "ServiceList",
		{Version: "v1", Resource: "configmaps"}:                 "ConfigMapList",
		{Version: "v1", Resource: "secrets"}:                    "SecretList",
		{Version: "v1", Resource: "serviceaccounts"}:            "ServiceAccountList",
		{Version: "v1", Resource: "endpoints"}:                  "EndpointsList",
		{Group: "apps", Version: "v1", Resource: "deployments"}: "DeploymentList",
		{Group: "apps", Version: "v1", Resource: "replicasets"}: "ReplicaSetList",
		{Group: "batch", Version: "v1", Resource: "jobs"}:       "JobList",
	}
}

func graphObject(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetAPIVersion(apiVersion)
	o.SetKind(kind)
	o.SetNamespace(namespace)
	o.SetName(name)
	o.SetUID(kubeUID(name))
	return o
}

func kubeUID(name string) types.UID { return types.UID(name + "-uid") }

func graphCluster(objects ...*unstructured.Unstructured) *fakeCluster {
	runtimeObjs := make([]runtime.Object, 0, len(objects))
	for _, o := range objects {
		runtimeObjs = append(runtimeObjs, o)
	}
	return &fakeCluster{
		active:  "ctx-a",
		dynamic: dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), graphListKinds(), runtimeObjs...),
	}
}

func graphRequest(target string) *http.Request {
	return chiRequest(http.MethodGet, target, "", map[string]string{"namespace": "web"})
}

func TestGraphHandlerServesTheGraph(t *testing.T) {
	controller := true
	rs := graphObject("apps/v1", "ReplicaSet", "web", "api-7d9")
	rs.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "apps/v1", Kind: "Deployment", Name: "api", UID: kubeUID("api"), Controller: &controller,
	}})
	cluster := graphCluster(graphObject("apps/v1", "Deployment", "web", "api"), rs)

	rec := httptest.NewRecorder()
	GraphHandler(graphDiscovery(), cluster, discardLogger())(rec,
		graphRequest("/api/v1/namespaces/web/graph?focus=Deployment/api&depth=1"))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var body graph.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	assert.Equal(t, "web", body.Namespace)
	assert.Equal(t, 1, body.Depth)
	assert.Equal(t, graph.Ref{
		Group: "apps", Version: "v1", Resource: "deployments",
		Kind: "Deployment", Namespace: "web", Name: "api",
	}, body.Focus)
	require.Len(t, body.Nodes, 2)
	assert.True(t, body.Nodes[0].Focus)
	assert.Equal(t, "ReplicaSet", body.Nodes[1].Kind)
	require.Len(t, body.Edges, 1)
	assert.Equal(t, graph.RelOwns, body.Edges[0].Relation)
	assert.False(t, body.Partial)
	assert.NotNil(t, body.Groups, "groups is always an array so the client never guards on null")
}

func TestGraphHandlerAcceptsPluralAndLowercaseFocusKinds(t *testing.T) {
	cluster := graphCluster(graphObject("apps/v1", "Deployment", "web", "api"))
	for _, focus := range []string{"Deployment/api", "deployment/api", "deployments/api", "DEPLOYMENTS/api"} {
		t.Run(focus, func(t *testing.T) {
			rec := httptest.NewRecorder()
			GraphHandler(graphDiscovery(), cluster, discardLogger())(rec,
				graphRequest("/api/v1/namespaces/web/graph?focus="+focus+"&depth=1"))
			assert.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		})
	}
}

func TestGraphHandlerRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		status int
		code   string
	}{
		{name: "no focus", query: "", status: http.StatusBadRequest, code: "invalid_focus"},
		{name: "focus without a name", query: "?focus=Deployment", status: http.StatusBadRequest, code: "invalid_focus"},
		{name: "focus with an empty name", query: "?focus=Deployment/", status: http.StatusBadRequest, code: "invalid_focus"},
		{name: "focus with an empty kind", query: "?focus=/api", status: http.StatusBadRequest, code: "invalid_focus"},
		{name: "non-numeric depth", query: "?focus=Deployment/api&depth=deep", status: http.StatusBadRequest, code: "invalid_depth"},
		{name: "zero depth", query: "?focus=Deployment/api&depth=0", status: http.StatusBadRequest, code: "invalid_depth"},
		{name: "negative depth", query: "?focus=Deployment/api&depth=-2", status: http.StatusBadRequest, code: "invalid_depth"},
		{name: "a kind the cluster does not serve", query: "?focus=Widget/api", status: http.StatusNotFound, code: "unknown_resource"},
		{name: "a cluster-scoped focus", query: "?focus=Node/kind-worker", status: http.StatusBadRequest, code: "invalid_scope"},
		{name: "an object that does not exist", query: "?focus=Deployment/ghost", status: http.StatusNotFound, code: "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			GraphHandler(graphDiscovery(), graphCluster(), discardLogger())(rec,
				graphRequest("/api/v1/namespaces/web/graph"+tt.query))
			assert.Equal(t, tt.status, rec.Code, "body: %s", rec.Body.String())
			assert.Equal(t, tt.code, errorCode(t, rec.Body.Bytes()))
		})
	}
}

func TestGraphHandlerClampsDepthRatherThanFailing(t *testing.T) {
	cluster := graphCluster(graphObject("apps/v1", "Deployment", "web", "api"))
	rec := httptest.NewRecorder()
	GraphHandler(graphDiscovery(), cluster, discardLogger())(rec,
		graphRequest("/api/v1/namespaces/web/graph?focus=Deployment/api&depth=99"))

	require.Equal(t, http.StatusOK, rec.Code)
	var body graph.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, graph.MaxDepth, body.Depth)
	assert.True(t, body.Partial, "a clamped depth is reported, not applied silently")
	assert.NotEmpty(t, body.Notes)
}

func TestGraphHandlerReportsInfrastructureFailures(t *testing.T) {
	t.Run("no kubeconfig is a 503", func(t *testing.T) {
		cluster := graphCluster()
		cluster.dynamic = nil
		cluster.dynamicErr = errNoKubeconfig
		rec := httptest.NewRecorder()
		GraphHandler(graphDiscovery(), cluster, discardLogger())(rec,
			graphRequest("/api/v1/namespaces/web/graph?focus=Deployment/api"))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "kubeconfig_unavailable", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("a discovery failure is classified", func(t *testing.T) {
		svc := NewDiscoveryService(&fakeDiscoveryCluster{
			active: "ctx-a", disc: &countingDiscovery{err: errUnreachable},
		})
		rec := httptest.NewRecorder()
		GraphHandler(svc, graphCluster(), discardLogger())(rec,
			graphRequest("/api/v1/namespaces/web/graph?focus=Deployment/api"))
		assert.Equal(t, http.StatusBadGateway, rec.Code)
		assert.Equal(t, "connection_refused", errorCode(t, rec.Body.Bytes()),
			"the graph reuses the engine's failure taxonomy rather than a bare 500")
	})
}

func TestGraphResolver(t *testing.T) {
	result, err := shapeDiscovery(graphAPILists, nil)
	require.NoError(t, err)
	resolver := newGraphResolver(result)

	t.Run("resolves by kind, plural and case-insensitively", func(t *testing.T) {
		for _, key := range []string{"Deployment", "deployment", "deployments", "DEPLOYMENTS"} {
			info, ok := resolver.ByKind(key)
			require.True(t, ok, key)
			assert.Equal(t, "apps", info.Group)
			assert.Equal(t, "deployments", info.Resource)
			assert.True(t, info.Namespaced)
		}
	})

	t.Run("reports what the cluster does not serve", func(t *testing.T) {
		_, ok := resolver.ByKind("Widget")
		assert.False(t, ok)
	})

	t.Run("carries scope through", func(t *testing.T) {
		info, ok := resolver.ByKind("Node")
		require.True(t, ok)
		assert.False(t, info.Namespaced)
	})

	t.Run("resolves an ownerReference apiVersion + kind", func(t *testing.T) {
		info, ok := resolver.ByGroupKind("apps/v1", "ReplicaSet")
		require.True(t, ok)
		assert.Equal(t, "replicasets", info.Resource)

		core, ok := resolver.ByGroupKind("v1", "Pod")
		require.True(t, ok)
		assert.Equal(t, "", core.Group, "a bare version means the core group")

		fallback, ok := resolver.ByGroupKind("", "Pod")
		require.True(t, ok)
		assert.Equal(t, "pods", fallback.Resource)
	})

	t.Run("an unknown group falls back to the kind", func(t *testing.T) {
		info, ok := resolver.ByGroupKind("mycompany.io/v1", "Deployment")
		require.True(t, ok)
		assert.Equal(t, "apps", info.Group)
	})
}

func TestGraphResolverPrefersTheBuiltInGroup(t *testing.T) {
	// A CRD shipping its own "Deployment" must not hijack an unqualified focus.
	lists := append([]*metav1.APIResourceList{
		apiList("acme.io/v1", apiRes("deployments", "Deployment", true, "get", "list")),
	}, graphAPILists...)
	result, err := shapeDiscovery(lists, nil)
	require.NoError(t, err)
	resolver := newGraphResolver(result)

	info, ok := resolver.ByKind("Deployment")
	require.True(t, ok)
	assert.Equal(t, "apps", info.Group)

	// Fully qualified, the CRD still resolves to itself.
	crd, ok := resolver.ByGroupKind("acme.io/v1", "Deployment")
	require.True(t, ok)
	assert.Equal(t, "acme.io", crd.Group)
}

func TestBetterGroup(t *testing.T) {
	assert.True(t, betterGroup("", "apps"), "the core group outranks everything")
	assert.True(t, betterGroup("apps", "acme.io"))
	assert.True(t, betterGroup("acme.io", "zulu.io"), "unranked groups fall back to alphabetical")
	assert.False(t, betterGroup("zulu.io", "acme.io"))
	assert.False(t, betterGroup("apps", "apps"))
}

func TestParseDepth(t *testing.T) {
	tests := []struct {
		raw     string
		want    int
		wantErr bool
	}{
		{raw: "", want: 0},
		{raw: "1", want: 1},
		{raw: "4", want: 4},
		{raw: "99", want: 99}, // clamped by the builder, not rejected here
		{raw: "0", wantErr: true},
		{raw: "-1", wantErr: true},
		{raw: "two", wantErr: true},
		{raw: "1.5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run("depth="+tt.raw, func(t *testing.T) {
			got, err := parseDepth(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
