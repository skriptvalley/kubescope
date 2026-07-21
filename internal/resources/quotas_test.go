package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestFormatQuotaQuantity(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"cpu", "1200m", "1.2"},
		{"cpu", "4", "4"},
		{"requests.cpu", "500m", "0.5"},
		{"limits.cpu", "2", "2"},
		{"memory", "2Gi", "2Gi"},
		{"requests.memory", "512Mi", "512Mi"},
		{"limits.memory", "8Gi", "8Gi"},
		{"pods", "10", "10"},
		{"services", "3", "3"},
		{"requests.storage", "20Gi", "20Gi"},
	}
	for _, tt := range tests {
		q := resource.MustParse(tt.in)
		assert.Equal(t, tt.want, formatQuotaQuantity(tt.name, q), "%s=%s", tt.name, tt.in)
	}
}

func TestQuotaPercent(t *testing.T) {
	tests := []struct {
		used, hard string
		want       int
	}{
		{"1200m", "4", 30},
		{"2Gi", "8Gi", 25},
		{"0", "4", 0},
		{"4", "4", 100},
		{"8", "4", 100}, // clamped
		{"1", "0", 0},   // unbounded
	}
	for _, tt := range tests {
		u := resource.MustParse(tt.used)
		h := resource.MustParse(tt.hard)
		assert.Equal(t, tt.want, quotaPercent(u, h), "%s/%s", tt.used, tt.hard)
	}
}
