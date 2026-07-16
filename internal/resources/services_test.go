package resources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func strptr(s string) *string { return &s }

func TestShapeService(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.5",
			Selector:  map[string]string{"app": "web"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			},
		},
	}
	detail := shapeService(svc)
	assert.Equal(t, "web", detail.Name)
	assert.Equal(t, "ClusterIP", detail.Type)
	assert.Equal(t, "10.0.0.5", detail.ClusterIP)
	assert.Equal(t, map[string]string{"app": "web"}, detail.Selector)
	require.Len(t, detail.Ports, 1)
	assert.Equal(t, int32(80), detail.Ports[0].Port)
	assert.Equal(t, "TCP", detail.Ports[0].Protocol)
	// Address slices are always non-nil so the JSON is a list, never null.
	assert.NotNil(t, detail.ReadyAddresses)
	assert.NotNil(t, detail.NotReadyAddresses)
}

func TestShapeEndpoints(t *testing.T) {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.1.0.3", NodeName: strptr("node-b"), TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "web-2"}},
					{IP: "10.1.0.1", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "web-1"}},
				},
				NotReadyAddresses: []corev1.EndpointAddress{
					{IP: "10.1.0.9", TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "web-3"}},
				},
			},
		},
	}
	ready, notReady := shapeEndpoints(ep)
	require.Len(t, ready, 2)
	// Sorted by IP for a stable render.
	assert.Equal(t, "10.1.0.1", ready[0].IP)
	assert.Equal(t, "web-1", ready[0].TargetRef.Name)
	assert.True(t, ready[0].Ready)
	assert.Equal(t, "node-b", ready[1].NodeName)
	require.Len(t, notReady, 1)
	assert.Equal(t, "10.1.0.9", notReady[0].IP)
	assert.False(t, notReady[0].Ready)
	assert.Equal(t, "web-3", notReady[0].TargetRef.Name)
}

func TestServiceDetailHandler(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.5", Selector: map[string]string{"app": "web"}},
	}
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{IP: "10.1.0.1", TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "web-1"}}},
		}},
	}

	t.Run("returns service summary + resolved endpoints", func(t *testing.T) {
		cluster := &fakeCluster{clientset: fake.NewClientset(svc, ep)}
		rec := httptest.NewRecorder()
		ServiceDetailHandler(cluster, discardLogger())(rec,
			chiRequest(http.MethodGet, "/", "", map[string]string{"namespace": "default", "name": "web"}))
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var detail ServiceDetail
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
		assert.Equal(t, "web", detail.Name)
		assert.True(t, detail.EndpointsFound)
		require.Len(t, detail.ReadyAddresses, 1)
		assert.Equal(t, "web-1", detail.ReadyAddresses[0].TargetRef.Name)
	})

	t.Run("missing endpoints object is not an error", func(t *testing.T) {
		cluster := &fakeCluster{clientset: fake.NewClientset(svc)} // no Endpoints
		rec := httptest.NewRecorder()
		ServiceDetailHandler(cluster, discardLogger())(rec,
			chiRequest(http.MethodGet, "/", "", map[string]string{"namespace": "default", "name": "web"}))
		require.Equal(t, http.StatusOK, rec.Code)

		var detail ServiceDetail
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
		assert.False(t, detail.EndpointsFound)
		assert.Empty(t, detail.ReadyAddresses)
	})

	t.Run("missing service is 404", func(t *testing.T) {
		cluster := &fakeCluster{clientset: fake.NewClientset()}
		rec := httptest.NewRecorder()
		ServiceDetailHandler(cluster, discardLogger())(rec,
			chiRequest(http.MethodGet, "/", "", map[string]string{"namespace": "default", "name": "ghost"}))
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "not_found", errorCode(t, rec.Body.Bytes()))
	})
}
