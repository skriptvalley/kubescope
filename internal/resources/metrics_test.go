package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
