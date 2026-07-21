package resources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestPodMetricsHandler(t *testing.T) {
	listKinds := map[schema.GroupVersionResource]string{podMetricsGVR: "PodMetricsList"}

	t.Run("degrades to unavailable (200) when the metrics API errors", func(t *testing.T) {
		dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
		// Simulate metrics-server absent: the metrics API list 404s.
		dc.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}, "")
		})
		rec := httptest.NewRecorder()
		PodMetricsHandler(&fakeCluster{dynamic: dc}, discardLogger())(
			rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics/pods", nil))

		require.Equal(t, http.StatusOK, rec.Code) // never breaks the view
		var body PodMetricsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.False(t, body.Available)
		assert.Empty(t, body.Items)
	})

	t.Run("returns summed per-pod usage when metrics-server is present", func(t *testing.T) {
		pm := unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "metrics.k8s.io/v1beta1",
			"kind":       "PodMetrics",
			"metadata":   map[string]interface{}{"name": "web-1", "namespace": "default"},
			"containers": []interface{}{
				map[string]interface{}{"name": "app", "usage": map[string]interface{}{"cpu": "10m", "memory": "32Mi"}},
			},
		}}
		// Return the list via a reactor — the metrics kind (PodMetrics) doesn't
		// pluralize to the "pods" resource, so the tracker won't index it under
		// podMetricsGVR; intercepting the list call is robust and exact.
		dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
		dc.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
			list := &unstructured.UnstructuredList{
				Object: map[string]interface{}{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetricsList"},
				Items:  []unstructured.Unstructured{pm},
			}
			return true, list, nil
		})
		rec := httptest.NewRecorder()
		PodMetricsHandler(&fakeCluster{dynamic: dc}, discardLogger())(
			rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics/pods", nil))

		require.Equal(t, http.StatusOK, rec.Code)
		var body PodMetricsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.True(t, body.Available)
		require.Len(t, body.Items, 1)
		assert.Equal(t, "web-1", body.Items[0].Name)
		assert.Equal(t, "10m", body.Items[0].CPU)
		assert.Equal(t, "32Mi", body.Items[0].Memory)
	})
}

func TestShapePodMetrics(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "api-gateway-abc", "namespace": "payments"},
		"containers": []interface{}{
			map[string]interface{}{"name": "app", "usage": map[string]interface{}{"cpu": "12m", "memory": "96Mi"}},
			map[string]interface{}{"name": "sidecar", "usage": map[string]interface{}{"cpu": "26m", "memory": "114Mi"}},
		},
	}}
	got := shapePodMetrics(u)
	assert.Equal(t, "api-gateway-abc", got.Name)
	assert.Equal(t, "payments", got.Namespace)
	assert.Equal(t, "38m", got.CPU)      // 12m + 26m
	assert.Equal(t, "210Mi", got.Memory) // 96Mi + 114Mi
}

func TestShapePodMetricsToleratesMissingAndBadValues(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "p", "namespace": "n"},
		"containers": []interface{}{
			map[string]interface{}{"name": "a", "usage": map[string]interface{}{"cpu": "not-a-qty"}},
			map[string]interface{}{"name": "b"}, // no usage
		},
	}}
	got := shapePodMetrics(u)
	assert.Equal(t, "0m", got.CPU)
	assert.Equal(t, "0Mi", got.Memory)
}

func TestFormatMebibytes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"96Mi", "96Mi"},
		{"512Mi", "512Mi"},
		{"1024Mi", "1.0Gi"},
		{"2100Mi", "2.1Gi"},
		{"0", "0Mi"},
	}
	for _, tt := range tests {
		q := resource.MustParse(tt.in)
		assert.Equal(t, tt.want, formatMebibytes(&q), tt.in)
	}
}
