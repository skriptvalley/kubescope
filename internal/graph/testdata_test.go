package graph

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

// Shared fixtures for the builder tests: a fake Resolver over the well-known
// kinds and a fake dynamic client that lists them. Objects are plain
// unstructured maps, so every value must be JSON-native (int64, not int) — the
// tracker deep-copies through DeepCopyJSON.

// fakeResolver resolves by lowercased kind, ignoring group qualification (the
// real resolver's group preference is tested in internal/resources).
type fakeResolver map[string]ResourceInfo

func (f fakeResolver) ByKind(kind string) (ResourceInfo, bool) {
	info, ok := f[strings.ToLower(kind)]
	return info, ok
}

func (f fakeResolver) ByGroupKind(_, kind string) (ResourceInfo, bool) { return f.ByKind(kind) }

func nsRes(group, version, resource, kind string) ResourceInfo {
	return ResourceInfo{Group: group, Version: version, Resource: resource, Kind: kind, Namespaced: true}
}

// stdResolver knows every kind the builder reasons about, so a test exercises
// the same code paths a real cluster would.
var stdResolver = fakeResolver{
	"pod":                     nsRes("", "v1", "pods", kindPod),
	"service":                 nsRes("", "v1", "services", kindService),
	"configmap":               nsRes("", "v1", "configmaps", kindConfigMap),
	"secret":                  nsRes("", "v1", "secrets", kindSecret),
	"serviceaccount":          nsRes("", "v1", "serviceaccounts", kindServiceAccount),
	"persistentvolumeclaim":   nsRes("", "v1", "persistentvolumeclaims", kindPVC),
	"endpoints":               nsRes("", "v1", "endpoints", kindEndpoints),
	"endpointslice":           nsRes("discovery.k8s.io", "v1", "endpointslices", kindEndpointSlice),
	"deployment":              nsRes("apps", "v1", "deployments", kindDeployment),
	"replicaset":              nsRes("apps", "v1", "replicasets", kindReplicaSet),
	"statefulset":             nsRes("apps", "v1", "statefulsets", kindStatefulSet),
	"daemonset":               nsRes("apps", "v1", "daemonsets", kindDaemonSet),
	"job":                     nsRes("batch", "v1", "jobs", kindJob),
	"cronjob":                 nsRes("batch", "v1", "cronjobs", kindCronJob),
	"ingress":                 nsRes("networking.k8s.io", "v1", "ingresses", kindIngress),
	"horizontalpodautoscaler": nsRes("autoscaling", "v2", "horizontalpodautoscalers", kindHPA),
	"persistentvolume": {
		Version: "v1", Resource: "persistentvolumes", Kind: "PersistentVolume", Namespaced: false,
	},
}

// listKinds registers every GVR the fake dynamic client may be asked to list.
var listKinds = func() map[schema.GroupVersionResource]string {
	out := map[schema.GroupVersionResource]string{}
	for _, info := range stdResolver {
		out[gvrOf(info)] = info.Kind + "List"
	}
	return out
}()

func fakeDynamic(objects ...*unstructured.Unstructured) *dynfake.FakeDynamicClient {
	runtimeObjs := make([]runtime.Object, 0, len(objects))
	for _, o := range objects {
		runtimeObjs = append(runtimeObjs, o)
	}
	return dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, runtimeObjs...)
}

// u builds a namespaced object with a deterministic UID of "<name>-uid".
func u(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	o.SetAPIVersion(apiVersion)
	o.SetKind(kind)
	if namespace != "" {
		o.SetNamespace(namespace)
	}
	o.SetName(name)
	o.SetUID(types.UID(name + "-uid"))
	return o
}

// ownedBy stamps a controller ownerReference matching u's UID convention.
func ownedBy(o *unstructured.Unstructured, apiVersion, kind, name string) *unstructured.Unstructured {
	controller := true
	o.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: apiVersion, Kind: kind, Name: name,
		UID: types.UID(name + "-uid"), Controller: &controller,
	}})
	return o
}

func withField(o *unstructured.Unstructured, value any, fields ...string) *unstructured.Unstructured {
	current := o.Object
	for _, f := range fields[:len(fields)-1] {
		next, ok := current[f].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[f] = next
		}
		current = next
	}
	current[fields[len(fields)-1]] = value
	return o
}

func withLabels(o *unstructured.Unstructured, labels map[string]string) *unstructured.Unstructured {
	o.SetLabels(labels)
	return o
}

// endpointSlice builds a ready-endpoint slice for a Service over the named pods.
func endpointSlice(namespace, service string, pods ...string) *unstructured.Unstructured {
	slice := withLabels(
		u("discovery.k8s.io/v1", kindEndpointSlice, namespace, service+"-slice"),
		map[string]string{serviceEndpointLabel: service},
	)
	endpoints := make([]any, 0, len(pods))
	for _, pod := range pods {
		endpoints = append(endpoints, map[string]any{
			"conditions": map[string]any{"ready": true},
			"targetRef":  map[string]any{"kind": kindPod, "name": pod, "namespace": namespace},
		})
	}
	slice.Object["endpoints"] = endpoints
	return slice
}

// podWithRefs builds a running pod consuming a ConfigMap and a Secret through
// both a volume and envFrom — the shape deploy/testenv's web/api pods have.
func podWithRefs(namespace, name, configMap, secret string) *unstructured.Unstructured {
	pod := u("v1", kindPod, namespace, name)
	pod.Object["spec"] = map[string]any{
		"serviceAccountName": "default",
		"volumes": []any{
			map[string]any{"name": "config", "configMap": map[string]any{"name": configMap}},
			map[string]any{"name": "creds", "secret": map[string]any{"secretName": secret}},
		},
		"containers": []any{map[string]any{
			"name": "app",
			"envFrom": []any{
				map[string]any{"configMapRef": map[string]any{"name": configMap}},
				map[string]any{"secretRef": map[string]any{"name": secret}},
			},
		}},
	}
	pod.Object["status"] = map[string]any{"phase": "Running"}
	return pod
}

// nodeByName finds the node for one named object. Aggregates stand for many
// objects and carry no name, so they never match.
func nodeByName(resp *Response, kind, name string) *Node {
	for i := range resp.Nodes {
		if !resp.Nodes[i].Aggregate && resp.Nodes[i].Kind == kind && resp.Nodes[i].Name == name {
			return &resp.Nodes[i]
		}
	}
	return nil
}

func edgeBetween(resp *Response, source, target *Node) *Edge {
	if source == nil || target == nil {
		return nil
	}
	for i := range resp.Edges {
		if resp.Edges[i].Source == source.ID && resp.Edges[i].Target == target.ID {
			return &resp.Edges[i]
		}
	}
	return nil
}

// nodeNames lists the individually-drawn objects of a kind — aggregates are
// excluded, so "no pod nodes" means the pods were clubbed, not that none exist.
func nodeNames(resp *Response, kind string) []string {
	var out []string
	for i := range resp.Nodes {
		if !resp.Nodes[i].Aggregate && resp.Nodes[i].Kind == kind {
			out = append(out, resp.Nodes[i].Name)
		}
	}
	return out
}

func notesJoined(resp *Response) string { return fmt.Sprint(resp.Notes) }

var _ dynamic.Interface = (*dynfake.FakeDynamicClient)(nil)
