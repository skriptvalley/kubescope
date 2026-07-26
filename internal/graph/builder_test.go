package graph

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
)

// webNamespace mirrors deploy/testenv's `web` namespace: an api Deployment with
// one ReplicaSet and two pods that consume a ConfigMap and a Secret through both
// a volume and envFrom, fronted by a Service.
func webNamespace() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		u("apps/v1", kindDeployment, "web", "api"),
		ownedBy(u("apps/v1", kindReplicaSet, "web", "api-7d9"), "apps/v1", kindDeployment, "api"),
		ownedBy(podWithRefs("web", "api-7d9-aaa", "frontend-config", "api-credentials"), "apps/v1", kindReplicaSet, "api-7d9"),
		ownedBy(podWithRefs("web", "api-7d9-bbb", "frontend-config", "api-credentials"), "apps/v1", kindReplicaSet, "api-7d9"),
		u("v1", kindConfigMap, "web", "frontend-config"),
		u("v1", kindSecret, "web", "api-credentials"),
		u("v1", kindServiceAccount, "web", "default"),
		u("v1", kindService, "web", "api"),
		endpointSlice("web", "api", "api-7d9-aaa", "api-7d9-bbb"),
	}
}

func buildWeb(t *testing.T, focusKind, name string, depth int) *Response {
	t.Helper()
	info, ok := stdResolver.ByKind(focusKind)
	require.True(t, ok)
	resp, err := Build(context.Background(), fakeDynamic(webNamespace()...), stdResolver, Options{
		Namespace: "web", Focus: info, Name: name, Depth: depth,
	})
	require.NoError(t, err)
	return resp
}

func TestBuildWalksAWorkloadNeighbourhood(t *testing.T) {
	resp := buildWeb(t, kindDeployment, "api", 0)

	assert.False(t, resp.Partial, "notes: %s", notesJoined(resp))
	assert.Equal(t, DefaultDepth, resp.Depth)

	deployment := nodeByName(resp, kindDeployment, "api")
	require.NotNil(t, deployment)
	assert.True(t, deployment.Focus)
	assert.Equal(t, 0, deployment.Depth)
	assert.Equal(t, "apps", deployment.Group)
	assert.Equal(t, "deployments", deployment.Resource)

	rs := nodeByName(resp, kindReplicaSet, "api-7d9")
	podA := nodeByName(resp, kindPod, "api-7d9-aaa")
	service := nodeByName(resp, kindService, "api")
	configMap := nodeByName(resp, kindConfigMap, "frontend-config")
	secret := nodeByName(resp, kindSecret, "api-credentials")
	account := nodeByName(resp, kindServiceAccount, "default")
	require.NotNil(t, rs)
	require.NotNil(t, podA)
	require.NotNil(t, service)
	require.NotNil(t, configMap)
	require.NotNil(t, secret)
	require.NotNil(t, account)

	assert.ElementsMatch(t, []string{"api-7d9-aaa", "api-7d9-bbb"}, nodeNames(resp, kindPod))
	assert.Equal(t, 1, rs.Depth)
	assert.Equal(t, 2, podA.Depth)
	assert.Equal(t, 3, service.Depth)
	assert.Equal(t, "Running", podA.Status)

	// Every relation the sprint calls for, in its true direction.
	assert.Equal(t, RelOwns, edgeBetween(resp, deployment, rs).Relation)
	assert.Equal(t, RelOwns, edgeBetween(resp, rs, podA).Relation)
	require.NotNil(t, edgeBetween(resp, service, podA), "the Service→Pod edge runs from the Service")
	assert.Equal(t, RelRoutes, edgeBetween(resp, service, podA).Relation)
	assert.Equal(t, "ready", edgeBetween(resp, service, podA).Label)
	assert.Equal(t, RelServiceAccount, edgeBetween(resp, podA, account).Relation)

	// One ConfigMap consumed two ways stays one edge whose label names both.
	cmEdge := edgeBetween(resp, podA, configMap)
	require.NotNil(t, cmEdge)
	assert.Equal(t, RelMounts, cmEdge.Relation)
	assert.Equal(t, "volume, envFrom", cmEdge.Label)
	assert.Equal(t, "volume, envFrom", edgeBetween(resp, podA, secret).Label)
}

func TestBuildBoxesAWorkloadIntoACompoundGroup(t *testing.T) {
	resp := buildWeb(t, kindDeployment, "api", 0)

	require.Len(t, resp.Groups, 1)
	group := resp.Groups[0]
	assert.Equal(t, "api", group.Label)
	assert.Equal(t, kindDeployment, group.Kind)
	assert.Equal(t, nodeByName(resp, kindDeployment, "api").ID, group.Root)

	// The Deployment circle contains its ReplicaSet, both pods and its Service.
	for _, n := range []*Node{
		nodeByName(resp, kindDeployment, "api"),
		nodeByName(resp, kindReplicaSet, "api-7d9"),
		nodeByName(resp, kindPod, "api-7d9-aaa"),
		nodeByName(resp, kindPod, "api-7d9-bbb"),
		nodeByName(resp, kindService, "api"),
	} {
		require.NotNil(t, n)
		assert.Equal(t, group.ID, n.Parent, "%s %s should sit inside the group", n.Kind, n.Name)
	}
	// Shared config is not owned by the workload, so it stays outside the box.
	assert.Empty(t, nodeByName(resp, kindConfigMap, "frontend-config").Parent)
	assert.Empty(t, nodeByName(resp, kindSecret, "api-credentials").Parent)
}

func TestBuildFromAPodWalksUpAndOut(t *testing.T) {
	resp := buildWeb(t, kindPod, "api-7d9-aaa", 0)

	focus := nodeByName(resp, kindPod, "api-7d9-aaa")
	require.NotNil(t, focus)
	assert.True(t, focus.Focus)

	rs := nodeByName(resp, kindReplicaSet, "api-7d9")
	deployment := nodeByName(resp, kindDeployment, "api")
	require.NotNil(t, rs)
	require.NotNil(t, deployment)
	assert.Equal(t, 1, rs.Depth)
	assert.Equal(t, 2, deployment.Depth)
	// The owner edge points owner → child whichever way the walk found it.
	assert.NotNil(t, edgeBetween(resp, rs, focus))
	assert.NotNil(t, edgeBetween(resp, deployment, rs))
	// The sibling pod is reached back through the ReplicaSet.
	assert.NotNil(t, nodeByName(resp, kindPod, "api-7d9-bbb"))
}

func TestBuildBoundsDepth(t *testing.T) {
	resp := buildWeb(t, kindDeployment, "api", 1)

	assert.Equal(t, 1, resp.Depth)
	assert.NotNil(t, nodeByName(resp, kindReplicaSet, "api-7d9"))
	assert.Empty(t, nodeNames(resp, kindPod), "pods are two hops away and must not appear at depth 1")
	assert.Nil(t, nodeByName(resp, kindConfigMap, "frontend-config"))
}

func TestBuildClampsDepthAboveTheMaximum(t *testing.T) {
	info, ok := stdResolver.ByKind(kindDeployment)
	require.True(t, ok)
	resp, err := Build(context.Background(), fakeDynamic(webNamespace()...), stdResolver, Options{
		Namespace: "web", Focus: info, Name: "api", Depth: MaxDepth + 5,
	})
	require.NoError(t, err)

	assert.Equal(t, MaxDepth, resp.Depth)
	assert.True(t, resp.Partial)
	assert.Contains(t, notesJoined(resp), "exceeds the maximum")
}

func TestBuildUsesTheServiceSelectorWhenThereAreNoEndpoints(t *testing.T) {
	pod := withLabels(u("v1", kindPod, "web", "frontend-1"), map[string]string{"app": "frontend"})
	pod.Object["status"] = map[string]any{"phase": "Pending"}
	service := withField(u("v1", kindService, "web", "frontend"),
		map[string]any{"app": "frontend"}, "spec", "selector")

	info, _ := stdResolver.ByKind(kindService)
	resp, err := Build(context.Background(), fakeDynamic(service, pod), stdResolver, Options{
		Namespace: "web", Focus: info, Name: "frontend",
	})
	require.NoError(t, err)

	edge := edgeBetween(resp, nodeByName(resp, kindService, "frontend"), nodeByName(resp, kindPod, "frontend-1"))
	require.NotNil(t, edge, "a Service with no endpoints still shows the pods its selector matches")
	assert.Equal(t, "selector", edge.Label, "selector-matched membership is labelled, not passed off as an endpoint")
}

func TestBuildClubsACronJobRunSeries(t *testing.T) {
	objects := []*unstructured.Unstructured{u("batch/v1", kindCronJob, "batch", "hourly-report")}
	for i := 1; i <= 3; i++ {
		job := ownedBy(u("batch/v1", kindJob, "batch", fmt.Sprintf("hourly-report-%d", i)), "batch/v1", kindCronJob, "hourly-report")
		job.Object["status"] = map[string]any{"succeeded": int64(1),
			"conditions": []any{map[string]any{"type": "Complete", "status": "True"}}}
		objects = append(objects, job)
	}
	info, _ := stdResolver.ByKind(kindCronJob)
	resp, err := Build(context.Background(), fakeDynamic(objects...), stdResolver, Options{
		Namespace: "batch", Focus: info, Name: "hourly-report",
	})
	require.NoError(t, err)

	assert.Empty(t, nodeNames(resp, kindJob), "the individual runs collapse into one node")
	var aggregate *Node
	for i := range resp.Nodes {
		if resp.Nodes[i].Aggregate {
			aggregate = &resp.Nodes[i]
		}
	}
	require.NotNil(t, aggregate)
	assert.Equal(t, kindJob, aggregate.Kind)
	assert.Equal(t, 3, aggregate.Count)
	assert.Equal(t, "3 Completed", aggregate.Status, "the aggregate carries the run outcomes, so a failure is not hidden")
	// Clubbing is a readability rule, not a truncation: nothing was dropped.
	assert.False(t, resp.Partial, "notes: %s", notesJoined(resp))
	assert.Equal(t, "runs", edgeBetween(resp, nodeByName(resp, kindCronJob, "hourly-report"), aggregate).Label)
}

func TestBuildClubsAJobsPodsButNotAReplicaSets(t *testing.T) {
	jobPods := []*unstructured.Unstructured{u("batch/v1", kindJob, "batch", "db-migrate")}
	for i := 1; i <= 2; i++ {
		jobPods = append(jobPods, ownedBy(u("v1", kindPod, "batch", fmt.Sprintf("db-migrate-%d", i)), "batch/v1", kindJob, "db-migrate"))
	}
	info, _ := stdResolver.ByKind(kindJob)
	resp, err := Build(context.Background(), fakeDynamic(jobPods...), stdResolver, Options{
		Namespace: "batch", Focus: info, Name: "db-migrate",
	})
	require.NoError(t, err)
	assert.Empty(t, nodeNames(resp, kindPod), "a Job's attempts are a run series and club")

	// A ReplicaSet's pods are peers, not runs — two of them stay two nodes, which
	// is what makes "Deployment → 3 pods" render as three circles.
	replicas := buildWeb(t, kindReplicaSet, "api-7d9", 1)
	assert.Len(t, nodeNames(replicas, kindPod), 2)
}

func TestBuildClubsAFanOutPastTheCapAndSaysSo(t *testing.T) {
	objects := []*unstructured.Unstructured{
		u("apps/v1", kindReplicaSet, "web", "wide"),
	}
	for i := 0; i < maxFanOut+1; i++ {
		objects = append(objects, ownedBy(u("v1", kindPod, "web", fmt.Sprintf("wide-%03d", i)), "apps/v1", kindReplicaSet, "wide"))
	}
	info, _ := stdResolver.ByKind(kindReplicaSet)
	resp, err := Build(context.Background(), fakeDynamic(objects...), stdResolver, Options{
		Namespace: "web", Focus: info, Name: "wide",
	})
	require.NoError(t, err)

	assert.Empty(t, nodeNames(resp, kindPod))
	require.Len(t, resp.Nodes, 2)
	assert.True(t, resp.Nodes[1].Aggregate)
	assert.Equal(t, maxFanOut+1, resp.Nodes[1].Count)
	assert.True(t, resp.Partial, "an over-cap fan-out is dropped detail and must be marked partial")
	assert.Contains(t, notesJoined(resp), "fan-out cap")
}

func TestBuildStopsAtTheNodeCapAndMarksPartial(t *testing.T) {
	// One pod with more dangling ConfigMap references than the graph may hold.
	pod := u("v1", kindPod, "web", "hungry")
	volumes := make([]any, 0, maxNodes*2)
	for i := 0; i < maxNodes*2; i++ {
		volumes = append(volumes, map[string]any{
			"name":      fmt.Sprintf("v%03d", i),
			"configMap": map[string]any{"name": fmt.Sprintf("cm-%03d", i)},
		})
	}
	pod.Object["spec"] = map[string]any{"volumes": volumes}

	info, _ := stdResolver.ByKind(kindPod)
	resp, err := Build(context.Background(), fakeDynamic(pod), stdResolver, Options{
		Namespace: "web", Focus: info, Name: "hungry", Depth: 1,
	})
	require.NoError(t, err)

	assert.Len(t, resp.Nodes, maxNodes)
	assert.True(t, resp.Partial)
	assert.Contains(t, notesJoined(resp), "node cap")
	// A referenced object that does not exist is a fact worth drawing.
	assert.True(t, resp.Nodes[1].Missing)
	assert.Equal(t, "Missing", resp.Nodes[1].Status)
}

func TestBuildMarksPartialWhenAListIsTruncated(t *testing.T) {
	objects := webNamespace()
	client := fakeDynamic(objects...)
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion("v1")
		list.SetKind("PodList")
		list.SetContinue("more-please")
		return true, list, nil
	})

	info, _ := stdResolver.ByKind(kindDeployment)
	resp, err := Build(context.Background(), client, stdResolver, Options{
		Namespace: "web", Focus: info, Name: "api",
	})
	require.NoError(t, err)
	assert.True(t, resp.Partial)
	assert.Contains(t, notesJoined(resp), "only the first")
}

func TestBuildDegradesWhenANeighbourKindCannotBeListed(t *testing.T) {
	client := fakeDynamic(webNamespace()...)
	client.PrependReactor("list", "endpointslices", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "discovery.k8s.io", Resource: "endpointslices"}, "", errForbidden{})
	})

	info, _ := stdResolver.ByKind(kindDeployment)
	resp, err := Build(context.Background(), client, stdResolver, Options{
		Namespace: "web", Focus: info, Name: "api",
	})
	require.NoError(t, err, "one unreadable relation type must not blank the whole graph")
	assert.True(t, resp.Partial)
	assert.Contains(t, notesJoined(resp), "could not list endpointslices")
	assert.NotNil(t, nodeByName(resp, kindReplicaSet, "api-7d9"), "the rest of the graph is still built")
}

// errForbidden stands in for the apiserver's RBAC denial cause.
type errForbidden struct{}

func (errForbidden) Error() string { return "forbidden" }

func TestBuildDerivesStorageRoutingAndScalingEdges(t *testing.T) {
	pod := u("v1", kindPod, "web", "store-0")
	pod.Object["spec"] = map[string]any{"volumes": []any{
		map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "data-store-0"}},
	}}
	claim := withField(u("v1", kindPVC, "web", "data-store-0"), "pvc-9f2", "spec", "volumeName")
	claim.Object["status"] = map[string]any{"phase": "Bound"}
	volume := u("v1", "PersistentVolume", "", "pvc-9f2")
	volume.Object["status"] = map[string]any{"phase": "Bound"}

	service := u("v1", kindService, "web", "api")
	ingress := u("networking.k8s.io/v1", kindIngress, "web", "public")
	ingress.Object["spec"] = map[string]any{"rules": []any{map[string]any{
		"http": map[string]any{"paths": []any{map[string]any{
			"backend": map[string]any{"service": map[string]any{"name": "api"}},
		}}},
	}}}

	deployment := u("apps/v1", kindDeployment, "web", "api")
	hpa := u("autoscaling/v2", kindHPA, "web", "api")
	hpa.Object["spec"] = map[string]any{"scaleTargetRef": map[string]any{
		"apiVersion": "apps/v1", "kind": kindDeployment, "name": "api",
	}}

	client := fakeDynamic(pod, claim, volume, service, ingress, deployment, hpa)

	t.Run("pod to claim to volume", func(t *testing.T) {
		info, _ := stdResolver.ByKind(kindPod)
		resp, err := Build(context.Background(), client, stdResolver, Options{
			Namespace: "web", Focus: info, Name: "store-0",
		})
		require.NoError(t, err)
		podNode := nodeByName(resp, kindPod, "store-0")
		claimNode := nodeByName(resp, kindPVC, "data-store-0")
		volumeNode := nodeByName(resp, "PersistentVolume", "pvc-9f2")
		require.NotNil(t, claimNode)
		require.NotNil(t, volumeNode)
		assert.Equal(t, RelClaims, edgeBetween(resp, podNode, claimNode).Relation)
		assert.Equal(t, RelClaims, edgeBetween(resp, claimNode, volumeNode).Relation)
		assert.Equal(t, "Bound", claimNode.Status)
		assert.Empty(t, volumeNode.Namespace, "a PersistentVolume is cluster-scoped")
	})

	t.Run("ingress to service", func(t *testing.T) {
		info, _ := stdResolver.ByKind(kindService)
		resp, err := Build(context.Background(), client, stdResolver, Options{
			Namespace: "web", Focus: info, Name: "api",
		})
		require.NoError(t, err)
		edge := edgeBetween(resp, nodeByName(resp, kindIngress, "public"), nodeByName(resp, kindService, "api"))
		require.NotNil(t, edge, "the edge runs Ingress → Service, whichever end is focused")
		assert.Equal(t, RelRoutes, edge.Relation)
	})

	t.Run("autoscaler to its target", func(t *testing.T) {
		info, _ := stdResolver.ByKind(kindDeployment)
		resp, err := Build(context.Background(), client, stdResolver, Options{
			Namespace: "web", Focus: info, Name: "api", Depth: 1,
		})
		require.NoError(t, err)
		edge := edgeBetween(resp, nodeByName(resp, kindHPA, "api"), nodeByName(resp, kindDeployment, "api"))
		require.NotNil(t, edge)
		assert.Equal(t, RelScales, edge.Relation)
	})
}

func TestBuildDerivesJobConfigEdgesFromItsPodTemplate(t *testing.T) {
	job := u("batch/v1", kindJob, "web", "config-sync")
	job.Object["spec"] = map[string]any{"template": map[string]any{"spec": map[string]any{
		"containers": []any{map[string]any{
			"name":    "sync",
			"envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "api-credentials"}}},
		}},
	}}}
	secret := u("v1", kindSecret, "web", "api-credentials")

	info, _ := stdResolver.ByKind(kindJob)
	resp, err := Build(context.Background(), fakeDynamic(job, secret), stdResolver, Options{
		Namespace: "web", Focus: info, Name: "config-sync", Depth: 1,
	})
	require.NoError(t, err)

	edge := edgeBetween(resp, nodeByName(resp, kindJob, "config-sync"), nodeByName(resp, kindSecret, "api-credentials"))
	require.NotNil(t, edge)
	assert.Equal(t, RelEnv, edge.Relation)
	assert.Equal(t, "envFrom", edge.Label)
}

func TestBuildReturnsTheApiserverErrorForAMissingFocus(t *testing.T) {
	info, _ := stdResolver.ByKind(kindDeployment)
	_, err := Build(context.Background(), fakeDynamic(), stdResolver, Options{
		Namespace: "web", Focus: info, Name: "nope",
	})
	require.Error(t, err)
	assert.True(t, apierrors.IsNotFound(err), "the handler classifies this into a 404; it must stay recognizable")
}

func TestMergeLabel(t *testing.T) {
	tests := []struct{ existing, add, want string }{
		{"", "volume", "volume"},
		{"volume", "", "volume"},
		{"volume", "envFrom", "volume, envFrom"},
		{"volume", "volume", "volume"},
		{"projected volume", "volume", "projected volume, volume"},
		{"volume, envFrom", "envFrom", "volume, envFrom"},
	}
	for _, tt := range tests {
		t.Run(tt.existing+"+"+tt.add, func(t *testing.T) {
			assert.Equal(t, tt.want, mergeLabel(tt.existing, tt.add))
		})
	}
}

func TestNodeIDIsStableAndScoped(t *testing.T) {
	pod := nsRes("", "v1", "pods", kindPod)
	assert.Equal(t, "core/Pod/web/api-1", nodeID(pod, "web", "api-1"))
	assert.Equal(t, "apps/Deployment/web/api", nodeID(nsRes("apps", "v1", "deployments", kindDeployment), "web", "api"))
	assert.True(t, strings.HasPrefix(nodeID(pod, "", "x"), "core/Pod//"))
}
