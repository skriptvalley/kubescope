package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestShapeStreamRow(t *testing.T) {
	t.Run("pod shapes to a typed PodSummary", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]any{"name": "web-1", "namespace": "default"},
			"spec":       map[string]any{"containers": []any{map[string]any{"name": "web", "image": "nginx"}}},
			"status": map[string]any{
				"phase": "Running",
				"containerStatuses": []any{
					map[string]any{"name": "web", "ready": true, "restartCount": int64(2), "state": map[string]any{"running": map[string]any{}}},
				},
			},
		}}

		row := ShapeStreamRow(gvrPods, u)
		summary, ok := row.(PodSummary)
		require.True(t, ok, "expected PodSummary, got %T", row)
		assert.Equal(t, "web-1", summary.Name)
		assert.Equal(t, "default", summary.Namespace)
		assert.Equal(t, "1/1", summary.Ready)
		assert.Equal(t, "Running", summary.Status)
		assert.Equal(t, int32(2), summary.Restarts)
	})

	t.Run("deployment shapes to a typed DeploymentSummary", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata":   map[string]any{"name": "web", "namespace": "default"},
			"spec":       map[string]any{"replicas": int64(3)},
			"status":     map[string]any{"readyReplicas": int64(2), "updatedReplicas": int64(3), "availableReplicas": int64(2)},
		}}

		row := ShapeStreamRow(gvrDeployments, u)
		summary, ok := row.(DeploymentSummary)
		require.True(t, ok, "expected DeploymentSummary, got %T", row)
		assert.Equal(t, "2/3", summary.Ready)
		assert.Equal(t, int32(3), summary.DesiredReplicas)
	})

	t.Run("event shapes to an EventFeedRow with involvedObject", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion":     "v1",
			"kind":           "Event",
			"metadata":       map[string]any{"name": "web-1.abc", "namespace": "default"},
			"type":           "Warning",
			"reason":         "BackOff",
			"message":        "back-off restarting failed container",
			"count":          int64(5),
			"lastTimestamp":  "2026-07-15T10:00:00Z",
			"involvedObject": map[string]any{"kind": "Pod", "namespace": "default", "name": "web-1"},
		}}

		row := ShapeStreamRow(gvrEvents, u)
		feed, ok := row.(EventFeedRow)
		require.True(t, ok, "expected EventFeedRow, got %T", row)
		assert.Equal(t, "web-1.abc", feed.Name)
		assert.Equal(t, "Warning", feed.Type)
		assert.Equal(t, "BackOff", feed.Reason)
		assert.Equal(t, int32(5), feed.Count)
		assert.Equal(t, InvolvedObjectRef{Kind: "Pod", Namespace: "default", Name: "web-1"}, feed.InvolvedObject)
	})

	t.Run("unknown GVR shapes to the generic metadata row", func(t *testing.T) {
		gvr := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.com/v1",
			"kind":       "Widget",
			"metadata":   map[string]any{"name": "w1", "namespace": "shop", "uid": "widget-uid"},
		}}

		row := ShapeStreamRow(gvr, u)
		generic, ok := row.(listRow)
		require.True(t, ok, "expected generic listRow, got %T", row)
		assert.Equal(t, "w1", generic.Name)
		assert.Equal(t, "shop", generic.Namespace)
		assert.Equal(t, "widget-uid", generic.UID)
	})

	t.Run("IsWorkloadStreamGVR distinguishes typed kinds", func(t *testing.T) {
		assert.True(t, IsWorkloadStreamGVR(gvrPods))
		assert.True(t, IsWorkloadStreamGVR(gvrCronJobs))
		assert.False(t, IsWorkloadStreamGVR(gvrEvents))
		assert.False(t, IsWorkloadStreamGVR(schema.GroupVersionResource{Resource: "configmaps", Version: "v1"}))
	})
}
