package resources

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

// Story 1.1 (FB-13): resolving a Service + port to its ready endpoint pods and
// each pod's concrete container port — the target set a service port-forward
// balances over.

func backendService(ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "web"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "frontend"}, Ports: ports},
	}
}

func backendEndpoints(subsets ...corev1.EndpointSubset) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "web"},
		Subsets:    subsets,
	}
}

func podAddress(name, ip string) corev1.EndpointAddress {
	return corev1.EndpointAddress{
		IP:        ip,
		TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "web", Name: name},
	}
}

func TestResolveServiceBackendsReadyOnly(t *testing.T) {
	client := fake.NewClientset(
		backendService(corev1.ServicePort{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}),
		backendEndpoints(corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{
				podAddress("frontend-b", "10.1.0.2"),
				podAddress("frontend-a", "10.1.0.1"),
			},
			NotReadyAddresses: []corev1.EndpointAddress{podAddress("frontend-c", "10.1.0.3")},
			Ports:             []corev1.EndpointPort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}},
		}),
	)

	backends, err := ResolveServiceBackends(context.Background(), client, "web", "frontend", 80)
	require.NoError(t, err)
	// Sorted by pod name; the not-ready pod is excluded, exactly as kube-proxy
	// excludes it from the ClusterIP rotation.
	assert.Equal(t, []ServiceBackend{
		{Namespace: "web", Pod: "frontend-a", Port: 8080},
		{Namespace: "web", Pod: "frontend-b", Port: 8080},
	}, backends)
}

func TestResolveServiceBackendsNamedTargetPortPerPod(t *testing.T) {
	// Two pods resolve the same named targetPort to different container ports,
	// so the endpoints controller splits them into subsets with different ports.
	client := fake.NewClientset(
		backendService(corev1.ServicePort{
			Name: "http", Port: 80, TargetPort: intstr.FromString("web"), Protocol: corev1.ProtocolTCP,
		}),
		backendEndpoints(
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{podAddress("frontend-a", "10.1.0.1")},
				Ports:     []corev1.EndpointPort{{Name: "http", Port: 8080}},
			},
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{podAddress("frontend-b", "10.1.0.2")},
				Ports:     []corev1.EndpointPort{{Name: "http", Port: 9090}},
			},
		),
	)

	backends, err := ResolveServiceBackends(context.Background(), client, "web", "frontend", 80)
	require.NoError(t, err)
	assert.Equal(t, []ServiceBackend{
		{Namespace: "web", Pod: "frontend-a", Port: 8080},
		{Namespace: "web", Pod: "frontend-b", Port: 9090},
	}, backends)
}

func TestResolveServiceBackendsMultiPortPicksTheNamedSubsetPort(t *testing.T) {
	client := fake.NewClientset(
		backendService(
			corev1.ServicePort{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			corev1.ServicePort{Name: "metrics", Port: 9100, Protocol: corev1.ProtocolTCP},
		),
		backendEndpoints(corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{podAddress("frontend-a", "10.1.0.1")},
			Ports: []corev1.EndpointPort{
				{Name: "http", Port: 8080},
				{Name: "metrics", Port: 9101},
			},
		}),
	)

	backends, err := ResolveServiceBackends(context.Background(), client, "web", "frontend", 9100)
	require.NoError(t, err)
	assert.Equal(t, []ServiceBackend{{Namespace: "web", Pod: "frontend-a", Port: 9101}}, backends)
}

func TestResolveServiceBackendsSingleUnnamedPort(t *testing.T) {
	client := fake.NewClientset(
		backendService(corev1.ServicePort{Port: 80}),
		backendEndpoints(corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{podAddress("frontend-a", "10.1.0.1")},
			Ports:     []corev1.EndpointPort{{Port: 8080}},
		}),
	)

	backends, err := ResolveServiceBackends(context.Background(), client, "web", "frontend", 80)
	require.NoError(t, err)
	assert.Equal(t, []ServiceBackend{{Namespace: "web", Pod: "frontend-a", Port: 8080}}, backends)
}

func TestResolveServiceBackendsPrefersTheTCPPortOfADualProtocolNumber(t *testing.T) {
	// kube-dns declares 53/UDP *before* 53/TCP. Matching on number alone would
	// pick the UDP entry and reject a port that is perfectly forwardable.
	client := fake.NewClientset(
		backendService(
			corev1.ServicePort{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
			corev1.ServicePort{Name: "dns-tcp", Port: 53, Protocol: corev1.ProtocolTCP},
		),
		backendEndpoints(corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{podAddress("frontend-a", "10.1.0.1")},
			Ports: []corev1.EndpointPort{
				{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
				{Name: "dns-tcp", Port: 53, Protocol: corev1.ProtocolTCP},
			},
		}),
	)

	backends, err := ResolveServiceBackends(context.Background(), client, "web", "frontend", 53)
	require.NoError(t, err)
	assert.Equal(t, []ServiceBackend{{Namespace: "web", Pod: "frontend-a", Port: 53}}, backends,
		"the TCP entry must win, and its name must drive the endpoints-subset join")
}

func TestResolveServiceBackendsReportsReadyButPodlessEndpoints(t *testing.T) {
	// A selector-less Service with manually managed Endpoints: the addresses are
	// ready, they just have no pod to forward to. Saying "no ready endpoints"
	// would contradict what the detail view shows.
	client := fake.NewClientset(
		backendService(corev1.ServicePort{Name: "http", Port: 80}),
		backendEndpoints(corev1.EndpointSubset{
			Addresses: []corev1.EndpointAddress{{IP: "203.0.113.7"}, {IP: "203.0.113.8"}},
			Ports:     []corev1.EndpointPort{{Name: "http", Port: 8080}},
		}),
	)

	_, err := ResolveServiceBackends(context.Background(), client, "web", "frontend", 80)
	var e *NoReadyEndpointsError
	require.ErrorAs(t, err, &e)
	assert.Equal(t, 2, e.NonPodReady)
	assert.Contains(t, err.Error(), "none backed by a pod")
	assert.NotContains(t, err.Error(), "has no ready endpoints")
}

func TestResolveServiceBackendsErrors(t *testing.T) {
	tests := []struct {
		name    string
		objects []runtime.Object
		port    int32
		assert  func(t *testing.T, err error)
	}{
		{
			name:    "service not found",
			objects: nil,
			port:    80,
			assert: func(t *testing.T, err error) {
				assert.True(t, apierrors.IsNotFound(err), "must stay a 404-class apierror through the wrap")
			},
		},
		{
			name: "port not declared on the service",
			objects: []runtime.Object{
				backendService(corev1.ServicePort{Name: "http", Port: 80}),
				backendEndpoints(corev1.EndpointSubset{
					Addresses: []corev1.EndpointAddress{podAddress("frontend-a", "10.1.0.1")},
					Ports:     []corev1.EndpointPort{{Name: "http", Port: 8080}},
				}),
			},
			port: 8443,
			assert: func(t *testing.T, err error) {
				var e *ServicePortNotFoundError
				require.ErrorAs(t, err, &e)
				assert.Equal(t, int32(8443), e.Port)
			},
		},
		{
			name:    "non-TCP port",
			objects: []runtime.Object{backendService(corev1.ServicePort{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP})},
			port:    53,
			assert: func(t *testing.T, err error) {
				var e *UnsupportedPortProtocolError
				require.ErrorAs(t, err, &e)
				assert.Equal(t, "UDP", e.Protocol)
			},
		},
		{
			name:    "no endpoints object",
			objects: []runtime.Object{backendService(corev1.ServicePort{Name: "http", Port: 80})},
			port:    80,
			assert: func(t *testing.T, err error) {
				var e *NoReadyEndpointsError
				assert.ErrorAs(t, err, &e)
			},
		},
		{
			name: "endpoints with only not-ready addresses",
			objects: []runtime.Object{
				backendService(corev1.ServicePort{Name: "http", Port: 80}),
				backendEndpoints(corev1.EndpointSubset{
					NotReadyAddresses: []corev1.EndpointAddress{podAddress("frontend-a", "10.1.0.1")},
					Ports:             []corev1.EndpointPort{{Name: "http", Port: 8080}},
				}),
			},
			port: 80,
			assert: func(t *testing.T, err error) {
				var e *NoReadyEndpointsError
				assert.ErrorAs(t, err, &e)
			},
		},
		{
			name: "ready address with no pod behind it",
			objects: []runtime.Object{
				backendService(corev1.ServicePort{Name: "http", Port: 80}),
				backendEndpoints(corev1.EndpointSubset{
					Addresses: []corev1.EndpointAddress{{IP: "10.1.0.9"}}, // external address, no targetRef
					Ports:     []corev1.EndpointPort{{Name: "http", Port: 8080}},
				}),
			},
			port: 80,
			assert: func(t *testing.T, err error) {
				var e *NoReadyEndpointsError
				assert.ErrorAs(t, err, &e, "an address with no pod behind it cannot be forwarded to")
			},
		},
		{
			name: "endpoints carry no port for the service port name",
			objects: []runtime.Object{
				backendService(corev1.ServicePort{Name: "http", Port: 80}),
				backendEndpoints(corev1.EndpointSubset{
					Addresses: []corev1.EndpointAddress{podAddress("frontend-a", "10.1.0.1")},
					Ports:     []corev1.EndpointPort{{Name: "metrics", Port: 9101}},
				}),
			},
			port: 80,
			assert: func(t *testing.T, err error) {
				var e *NoReadyEndpointsError
				assert.ErrorAs(t, err, &e)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewClientset(tc.objects...)
			_, err := ResolveServiceBackends(context.Background(), client, "web", "frontend", tc.port)
			require.Error(t, err)
			tc.assert(t, err)
		})
	}
}
