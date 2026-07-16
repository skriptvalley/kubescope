package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func u(obj map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: obj}
}

// cellsFor runs the registered enrichment for a GVR over an object, returning
// the computed cells — the exact shape shapeList/genericStreamRow embed.
func cellsFor(group, resource string, obj map[string]any) map[string]string {
	specs := enrichmentFor(schema.GroupVersionResource{Group: group, Resource: resource})
	return enrichRow(specs, u(obj))
}

func TestEnrichmentForKnownAndUnknown(t *testing.T) {
	assert.NotEmpty(t, enrichmentFor(schema.GroupVersionResource{Group: "", Resource: "services"}))
	assert.NotEmpty(t, enrichmentFor(schema.GroupVersionResource{Group: "networking.k8s.io", Resource: "ingresses"}))
	// A version difference must not matter — keyed by group+resource.
	assert.NotEmpty(t, enrichmentFor(schema.GroupVersionResource{Group: "", Version: "v1beta1", Resource: "services"}))
	// Unknown kind (and CRDs) get no enrichment → default name/namespace/age.
	assert.Nil(t, enrichmentFor(schema.GroupVersionResource{Group: "example.com", Resource: "widgets"}))
	assert.Nil(t, enrichRow(nil, u(map[string]any{})))
}

func TestServiceColumns(t *testing.T) {
	cells := cellsFor("", "services", map[string]any{
		"spec": map[string]any{
			"type":      "NodePort",
			"clusterIP": "10.0.0.1",
			"selector":  map[string]any{"app": "web", "tier": "frontend"},
			"ports": []any{
				map[string]any{"port": int64(80), "protocol": "TCP"},
				map[string]any{"port": int64(443)}, // protocol defaults to TCP
			},
		},
	})
	assert.Equal(t, "NodePort", cells["type"])
	assert.Equal(t, "10.0.0.1", cells["cluster-ip"])
	assert.Equal(t, "80/TCP, 443/TCP", cells["ports"])
	assert.Equal(t, "app=web,tier=frontend", cells["selector"]) // sorted
}

func TestServiceColumnsDefaultsAndEmpties(t *testing.T) {
	cells := cellsFor("", "services", map[string]any{
		"spec": map[string]any{"clusterIP": "None"}, // headless, no type/ports/selector
	})
	assert.Equal(t, "ClusterIP", cells["type"], "type defaults to ClusterIP")
	assert.Equal(t, "None", cells["cluster-ip"])
	assert.Equal(t, "—", cells["ports"])
	assert.Equal(t, "—", cells["selector"])
}

func TestConfigMapAndSecretColumns(t *testing.T) {
	cm := cellsFor("", "configmaps", map[string]any{
		"data":       map[string]any{"a": "1", "b": "2"},
		"binaryData": map[string]any{"c": "AA=="},
	})
	assert.Equal(t, "3", cm["data"])

	// Secret enrichment reads only the key COUNT + type, never a value (ADR-0005).
	sec := cellsFor("", "secrets", map[string]any{
		"type": "kubernetes.io/tls",
		"data": map[string]any{"tls.crt": "c2VjcmV0", "tls.key": "c2VjcmV0"},
	})
	assert.Equal(t, "kubernetes.io/tls", sec["type"])
	assert.Equal(t, "2", sec["data"])
	for _, v := range sec {
		assert.NotContains(t, v, "c2VjcmV0", "no Secret data value may appear in a list cell")
	}
}

func TestIngressColumns(t *testing.T) {
	cells := cellsFor("networking.k8s.io", "ingresses", map[string]any{
		"spec": map[string]any{
			"rules": []any{
				map[string]any{
					"host": "example.com",
					"http": map[string]any{"paths": []any{
						map[string]any{"path": "/", "backend": map[string]any{
							"service": map[string]any{"name": "web", "port": map[string]any{"number": int64(80)}},
						}},
						map[string]any{"path": "/api", "backend": map[string]any{
							"service": map[string]any{"name": "api", "port": map[string]any{"number": int64(8080)}},
						}},
					}},
				},
			},
		},
	})
	assert.Equal(t, "example.com", cells["hosts"])
	assert.Equal(t, "/, /api", cells["paths"])
	assert.Equal(t, "web:80, api:8080", cells["backends"])
}

func TestIngressColumnsCatchAllAndNamedPort(t *testing.T) {
	cells := cellsFor("networking.k8s.io", "ingresses", map[string]any{
		"spec": map[string]any{
			"defaultBackend": map[string]any{
				"service": map[string]any{"name": "fallback", "port": map[string]any{"name": "http"}},
			},
		},
	})
	assert.Equal(t, "*", cells["hosts"], "no rules → catch-all host")
	assert.Equal(t, "—", cells["paths"])
	assert.Equal(t, "fallback:http", cells["backends"], "named port renders by name")
}

func TestPVCColumns(t *testing.T) {
	cells := cellsFor("", "persistentvolumeclaims", map[string]any{
		"spec": map[string]any{
			"volumeName":       "pv-1",
			"storageClassName": "standard",
			"accessModes":      []any{"ReadWriteOnce"},
		},
		"status": map[string]any{
			"phase":    "Bound",
			"capacity": map[string]any{"storage": "1Gi"},
		},
	})
	assert.Equal(t, "Bound", cells["status"])
	assert.Equal(t, "pv-1", cells["volume"])
	assert.Equal(t, "1Gi", cells["capacity"])
	assert.Equal(t, "RWO", cells["access-modes"])
	assert.Equal(t, "standard", cells["storageclass"])
}

func TestPVCColumnsPending(t *testing.T) {
	// An unbound/pending PVC still renders a meaningful status, not blanks.
	cells := cellsFor("", "persistentvolumeclaims", map[string]any{
		"spec":   map[string]any{"accessModes": []any{"ReadWriteMany", "ReadOnlyMany"}},
		"status": map[string]any{"phase": "Pending"},
	})
	assert.Equal(t, "Pending", cells["status"])
	assert.Equal(t, "—", cells["volume"])
	assert.Equal(t, "—", cells["capacity"])
	assert.Equal(t, "RWX, ROX", cells["access-modes"])
	assert.Equal(t, "—", cells["storageclass"])
}

func TestPVColumns(t *testing.T) {
	cells := cellsFor("", "persistentvolumes", map[string]any{
		"spec": map[string]any{
			"capacity":                      map[string]any{"storage": "10Gi"},
			"accessModes":                   []any{"ReadWriteOnce"},
			"persistentVolumeReclaimPolicy": "Retain",
			"storageClassName":              "standard",
			"claimRef":                      map[string]any{"namespace": "default", "name": "data-0"},
		},
		"status": map[string]any{"phase": "Bound"},
	})
	assert.Equal(t, "10Gi", cells["capacity"])
	assert.Equal(t, "RWO", cells["access-modes"])
	assert.Equal(t, "Retain", cells["reclaim-policy"])
	assert.Equal(t, "Bound", cells["status"])
	assert.Equal(t, "default/data-0", cells["claim"])
	assert.Equal(t, "standard", cells["storageclass"])
}

func TestPVColumnsUnbound(t *testing.T) {
	cells := cellsFor("", "persistentvolumes", map[string]any{
		"spec":   map[string]any{"capacity": map[string]any{"storage": "5Gi"}},
		"status": map[string]any{"phase": "Available"},
	})
	assert.Equal(t, "Available", cells["status"])
	assert.Equal(t, "—", cells["claim"], "an unbound PV has no claim")
}

func TestStorageClassColumns(t *testing.T) {
	def := cellsFor("storage.k8s.io", "storageclasses", map[string]any{
		"provisioner": "kubernetes.io/aws-ebs",
		"metadata": map[string]any{"annotations": map[string]any{
			"storageclass.kubernetes.io/is-default-class": "true",
		}},
	})
	assert.Equal(t, "kubernetes.io/aws-ebs", def["provisioner"])
	assert.Equal(t, "Delete", def["reclaim-policy"], "reclaim policy defaults to Delete")
	assert.Equal(t, "Immediate", def["binding-mode"], "binding mode defaults to Immediate")
	assert.Equal(t, "Yes", def["default"])

	nonDefault := cellsFor("storage.k8s.io", "storageclasses", map[string]any{
		"provisioner":       "rancher.io/local-path",
		"reclaimPolicy":     "Retain",
		"volumeBindingMode": "WaitForFirstConsumer",
	})
	assert.Equal(t, "Retain", nonDefault["reclaim-policy"])
	assert.Equal(t, "WaitForFirstConsumer", nonDefault["binding-mode"])
	assert.Equal(t, "", nonDefault["default"])
}

func TestRBACColumns(t *testing.T) {
	role := cellsFor("rbac.authorization.k8s.io", "roles", map[string]any{
		"rules": []any{map[string]any{"verbs": []any{"get"}}, map[string]any{"verbs": []any{"list"}}},
	})
	assert.Equal(t, "2", role["rules"])

	binding := cellsFor("rbac.authorization.k8s.io", "rolebindings", map[string]any{
		"roleRef": map[string]any{"kind": "ClusterRole", "name": "view"},
	})
	assert.Equal(t, "ClusterRole/view", binding["role"])

	sa := cellsFor("", "serviceaccounts", map[string]any{
		"secrets": []any{map[string]any{"name": "token-1"}},
	})
	assert.Equal(t, "1", sa["secrets"])
}

func TestShapeListEnrichesColumnsAndCells(t *testing.T) {
	// End-to-end through shapeList: enriched columns sit between namespace and age,
	// and each row carries the matching cells.
	info := APIResourceInfo{Group: "", Version: "v1", Resource: "services", Kind: "Service", Namespaced: true}
	list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		{Object: map[string]any{
			"metadata": map[string]any{"name": "web", "namespace": "default"},
			"spec":     map[string]any{"type": "ClusterIP", "clusterIP": "10.0.0.5"},
		}},
	}}
	resp := shapeList(info, list)

	var ids []string
	for _, c := range resp.Columns {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"name", "namespace", "type", "cluster-ip", "ports", "selector", "age"}, ids)

	require.Len(t, resp.Rows, 1)
	assert.Equal(t, "web", resp.Rows[0].Name)
	assert.Equal(t, "ClusterIP", resp.Rows[0].Cells["type"])
	assert.Equal(t, "10.0.0.5", resp.Rows[0].Cells["cluster-ip"])
}

func TestShapeListNoEnrichmentOmitsCells(t *testing.T) {
	info := APIResourceInfo{Group: "example.com", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true}
	list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		{Object: map[string]any{"metadata": map[string]any{"name": "w1", "namespace": "default"}}},
	}}
	resp := shapeList(info, list)
	assert.Equal(t, []listColumn{{ID: "name", Header: "Name"}, {ID: "namespace", Header: "Namespace"}, {ID: "age", Header: "Age"}}, resp.Columns)
	assert.Nil(t, resp.Rows[0].Cells, "a kind with no enrichment carries no cells map")
}
