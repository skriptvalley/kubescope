package resources

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type stubProvider struct {
	clientset kubernetes.Interface
	err       error
}

func (s *stubProvider) Clientset() (kubernetes.Interface, error) { return s.clientset, s.err }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func node(name string, ready corev1.ConditionStatus, version string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: ready}},
			NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: version},
		},
	}
}

func TestNodesHandler(t *testing.T) {
	tests := []struct {
		name       string
		provider   ClientsetProvider
		wantStatus int
		wantItems  []NodeSummary
		wantCode   string
	}{
		{
			name: "nodes with mixed readiness",
			provider: &stubProvider{clientset: fake.NewClientset(
				node("worker-a", corev1.ConditionTrue, "v1.31.0"),
				node("worker-b", corev1.ConditionFalse, "v1.31.0"),
				node("worker-c", corev1.ConditionUnknown, "v1.30.2"),
			)},
			wantStatus: http.StatusOK,
			wantItems: []NodeSummary{
				{Name: "worker-a", Status: "Ready", Version: "v1.31.0"},
				{Name: "worker-b", Status: "NotReady", Version: "v1.31.0"},
				{Name: "worker-c", Status: "Unknown", Version: "v1.30.2"},
			},
		},
		{
			name: "node without ready condition reports unknown",
			provider: &stubProvider{clientset: fake.NewClientset(&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "bare"},
				Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.29.0"}},
			})},
			wantStatus: http.StatusOK,
			wantItems:  []NodeSummary{{Name: "bare", Status: "Unknown", Version: "v1.29.0"}},
		},
		{
			name:       "empty cluster returns empty items, not null",
			provider:   &stubProvider{clientset: fake.NewClientset()},
			wantStatus: http.StatusOK,
			wantItems:  []NodeSummary{},
		},
		{
			name:       "kubeconfig unavailable is structured 503",
			provider:   &stubProvider{err: errors.New("loading kubeconfig \"/kubeconfig\": stat /kubeconfig: no such file")},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "kubeconfig_unavailable",
		},
		{
			name: "cluster unreachable is structured 502",
			provider: &stubProvider{clientset: func() kubernetes.Interface {
				cs := fake.NewClientset()
				cs.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("connection refused")
				})
				return cs
			}()},
			wantStatus: http.StatusBadGateway,
			wantCode:   "cluster_unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
			NodesHandler(tt.provider, discardLogger())(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			if tt.wantCode != "" {
				var envelope struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
				assert.Equal(t, tt.wantCode, envelope.Error.Code)
				assert.NotEmpty(t, envelope.Error.Message)
				return
			}

			var body struct {
				Items []NodeSummary `json:"items"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.wantItems, body.Items)
			assert.Contains(t, rec.Body.String(), `"items":[`, "items must serialize as an array even when empty")
		})
	}
}
