package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/skriptvalley/kubescope/internal/resources"
)

// Story 1.2/1.3 (FB-13): the load-balancing front for a Service's ready
// endpoints. Real loopback listeners stand in for the backend pods so the splice
// path is exercised end to end; the per-pod forwards themselves are the same
// injected fakes the pod-forward tests use.

// fakeBackendPod is one stand-in backend: a loopback server that answers every
// connection with its own marker, plus the fake forwarder that "reaches" it.
type fakeBackendPod struct {
	pod    string
	marker string
	fwd    *fakeForwarder
}

// newFakeBackendPods starts one marker server per pod and pairs it with a fake
// forwarder reporting that server's port as its local port.
func newFakeBackendPods(t *testing.T, pods ...string) map[string]*fakeBackendPod {
	t.Helper()
	out := make(map[string]*fakeBackendPod, len(pods))
	for _, pod := range pods {
		listener, err := net.Listen("tcp", net.JoinHostPort(pfLoopback, "0"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = listener.Close() })

		marker := pod
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				go func() {
					defer func() { _ = conn.Close() }()
					_, _ = io.WriteString(conn, marker)
				}()
			}
		}()
		port := uint16(listener.Addr().(*net.TCPAddr).Port)
		out[pod] = &fakeBackendPod{pod: pod, marker: marker, fwd: newFakeForwarder(port)}
	}
	return out
}

// backendFactory hands each per-pod forward request its pre-built fake.
func backendFactory(pods map[string]*fakeBackendPod) forwarderFactory {
	return func(target forwardTarget) (forwarder, error) {
		b, ok := pods[target.pod]
		if !ok {
			return nil, fmt.Errorf("no fake backend for pod %q", target.pod)
		}
		return b.fwd, nil
	}
}

func serviceBackends(pods ...string) []resources.ServiceBackend {
	out := make([]resources.ServiceBackend, 0, len(pods))
	for _, p := range pods {
		out = append(out, resources.ServiceBackend{Namespace: "web", Pod: p, Port: 80})
	}
	return out
}

// dialMarker opens one connection to the balancer and reads which backend served
// it. The marker server closes its side, so the read ends at EOF.
func dialMarker(t *testing.T, port uint16) string {
	t.Helper()
	conn, err := net.Dial("tcp", net.JoinHostPort(pfLoopback, strconv.Itoa(int(port))))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	body, err := io.ReadAll(conn)
	require.NoError(t, err)
	return string(body)
}

func dialMarkers(t *testing.T, port uint16, n int) []string {
	t.Helper()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, dialMarker(t, port))
	}
	return out
}

func tally(markers []string) map[string]int {
	out := map[string]int{}
	for _, m := range markers {
		out[m]++
	}
	return out
}

func TestServiceForwarderRoundRobinsEachConnection(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a", "frontend-b", "frontend-c")
	sf, err := newServiceForwarder(backendFactory(pods), forwardTarget{},
		serviceBackends("frontend-a", "frontend-b", "frontend-c"), 0)
	require.NoError(t, err)
	defer sf.stop()

	assert.NotZero(t, sf.localPort(), "the balancer must bind one public loopback port")
	assert.Equal(t, 3, sf.backendCount())

	markers := dialMarkers(t, sf.localPort(), 6)
	assert.Equal(t, map[string]int{"frontend-a": 2, "frontend-b": 2, "frontend-c": 2}, tally(markers),
		"each new connection goes to the next backend, so 6 connections split evenly over 3")
	assert.Equal(t, markers[:3], markers[3:], "the rotation must repeat in the same order")
}

func TestServiceForwarderDropsADeadBackendFromRotation(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a", "frontend-b", "frontend-c")
	sf, err := newServiceForwarder(backendFactory(pods), forwardTarget{},
		serviceBackends("frontend-a", "frontend-b", "frontend-c"), 0)
	require.NoError(t, err)
	defer sf.stop()

	// frontend-b's pod is deleted mid-forward: its forward exits on its own.
	pods["frontend-b"].fwd.die()
	require.Eventually(t, func() bool { return sf.backendCount() == 2 }, time.Second, 10*time.Millisecond,
		"a dead backend must drop out of the live count")

	markers := dialMarkers(t, sf.localPort(), 6)
	assert.Equal(t, map[string]int{"frontend-a": 3, "frontend-c": 3}, tally(markers),
		"the dead backend must be skipped; the survivors keep round-robining")

	select {
	case <-sf.done():
		t.Fatal("the session must survive while at least one backend is live")
	default:
	}
}

func TestServiceForwarderClosesWhenTheLastBackendDies(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a", "frontend-b")
	sf, err := newServiceForwarder(backendFactory(pods), forwardTarget{},
		serviceBackends("frontend-a", "frontend-b"), 0)
	require.NoError(t, err)
	defer sf.stop()

	pods["frontend-a"].fwd.die()
	select {
	case <-sf.done():
		t.Fatal("one dead backend must not end the session")
	case <-time.After(50 * time.Millisecond):
	}

	pods["frontend-b"].fwd.die()
	select {
	case <-sf.done():
	case <-time.After(time.Second):
		t.Fatal("the last backend dying must close the session, like a dead pod forward")
	}
}

func TestServiceForwarderStopTearsDownEveryBackend(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a", "frontend-b")
	sf, err := newServiceForwarder(backendFactory(pods), forwardTarget{},
		serviceBackends("frontend-a", "frontend-b"), 0)
	require.NoError(t, err)

	local := sf.localPort()
	sf.stop()
	sf.stop() // idempotent

	for name, b := range pods {
		assert.True(t, b.fwd.stopped.Load(), "stop must tear down the per-pod forward for %s", name)
	}
	select {
	case <-sf.done():
	case <-time.After(time.Second):
		t.Fatal("stop must close the session")
	}
	_, err = net.Dial("tcp", net.JoinHostPort(pfLoopback, strconv.Itoa(int(local))))
	assert.Error(t, err, "the public listener must be closed")
}

// flakyListener fails the first `fail` accepts with a transient error (the shape
// of fd exhaustion) and counts every Accept call.
type flakyListener struct {
	net.Listener
	fail     int
	failed   atomic.Int64
	accepted atomic.Int64
}

func (l *flakyListener) Accept() (net.Conn, error) {
	l.accepted.Add(1)
	if l.failed.Load() < int64(l.fail) {
		l.failed.Add(1)
		return nil, syscall.EMFILE
	}
	return l.Listener.Accept()
}

// newFlakyForwarder hand-builds a serviceForwarder over an injected listener, so
// the accept loop can be tested without racing the one newServiceForwarder
// starts.
func newFlakyForwarder(t *testing.T, backend *fakeBackendPod, fail int) (*serviceForwarder, *flakyListener) {
	t.Helper()
	raw, err := net.Listen("tcp", net.JoinHostPort(pfLoopback, "0"))
	require.NoError(t, err)
	flaky := &flakyListener{Listener: raw, fail: fail}

	sf := &serviceForwarder{
		listener: flaky,
		local:    uint16(raw.Addr().(*net.TCPAddr).Port),
		backends: []*serviceBackend{{
			pod:  backend.pod,
			addr: net.JoinHostPort(pfLoopback, strconv.Itoa(int(backend.fwd.localPort()))),
			fwd:  backend.fwd,
		}},
		conns:  make(map[net.Conn]struct{}),
		doneCh: make(chan struct{}),
	}
	go sf.acceptLoop()
	return sf, flaky
}

func TestServiceForwarderSurvivesATransientAcceptFailure(t *testing.T) {
	// A blip (fd exhaustion) must not destroy a working forward: the loop backs
	// off and retries rather than treating every accept error as fatal.
	pods := newFakeBackendPods(t, "frontend-a")
	sf, flaky := newFlakyForwarder(t, pods["frontend-a"], 3)
	defer sf.stop()

	assert.Equal(t, "frontend-a", dialMarker(t, sf.localPort()),
		"the session must still serve after transient accept failures")
	assert.Equal(t, int64(3), flaky.failed.Load(), "the test must actually have injected failures")
	select {
	case <-sf.done():
		t.Fatal("a transient accept failure must not end the session")
	default:
	}
}

func TestServiceForwarderAcceptLoopExitsOnAClosedListener(t *testing.T) {
	// A closed listener is the deliberate teardown, not a retryable failure: the
	// loop must exit rather than spin on ErrClosed until the failure cap.
	pods := newFakeBackendPods(t, "frontend-a")
	sf, flaky := newFlakyForwarder(t, pods["frontend-a"], 0)

	sf.stop()
	time.Sleep(100 * time.Millisecond)
	// The one parked Accept returns ErrClosed and the loop exits. A retry loop
	// would instead burn through the failure cap (≥2 calls) before giving up.
	assert.Equal(t, int64(1), flaky.accepted.Load(),
		"the loop must stop calling Accept once the listener is closed")
}

func TestServiceForwarderStopClosesInFlightConnections(t *testing.T) {
	// A backend that accepts and then goes silent would park splice forever if
	// stop only owned the client half of the pair.
	silent, err := net.Listen("tcp", net.JoinHostPort(pfLoopback, "0"))
	require.NoError(t, err)
	defer func() { _ = silent.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := silent.Accept()
		if err != nil {
			return
		}
		accepted <- conn // hold it open, never write
	}()

	fwd := newFakeForwarder(uint16(silent.Addr().(*net.TCPAddr).Port))
	sf, _ := newFlakyForwarder(t, &fakeBackendPod{pod: "frontend-a", fwd: fwd}, 0)

	client, err := net.Dial("tcp", net.JoinHostPort(pfLoopback, strconv.Itoa(int(sf.localPort()))))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	select {
	case conn := <-accepted:
		defer func() { _ = conn.Close() }()
	case <-time.After(2 * time.Second):
		t.Fatal("the balancer must have dialed the backend")
	}

	sf.stop()
	require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err = client.Read(make([]byte, 1))
	require.Error(t, err, "stop must close the client connection")
	assert.NotErrorIs(t, err, os.ErrDeadlineExceeded, "stop must not leave the connection parked")
}

func TestServiceForwarderFailedBackendStopsTheRest(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a")
	// frontend-b has no fake, so its forward fails to establish.
	_, err := newServiceForwarder(backendFactory(pods), forwardTarget{},
		serviceBackends("frontend-a", "frontend-b"), 0)
	require.Error(t, err)
	assert.True(t, pods["frontend-a"].fwd.stopped.Load(),
		"a failed start must leave nothing running")
}

// --- Manager-level service sessions (Story 1.3) ------------------------------

// fakeServiceCluster is the pod-forward test cluster with a seeded clientset, so
// the endpoint resolution inside start() runs against real Service/Endpoints
// objects.
type fakeServiceCluster struct {
	fakePFCluster
	client kubernetes.Interface
}

func (c *fakeServiceCluster) ClientsetFor(string) (kubernetes.Interface, error) {
	return c.client, nil
}

func newFakeServiceCluster(ctxName string, objects ...runtime.Object) *fakeServiceCluster {
	c := &fakeServiceCluster{client: fake.NewClientset(objects...)}
	c.setContext(ctxName)
	return c
}

// frontendService is the sprint's dogfood shape: one Service fronting three
// ready nginx pods (the testenv `web/frontend` fixture).
func frontendService(pods ...string) []runtime.Object {
	addresses := make([]corev1.EndpointAddress, 0, len(pods))
	for _, p := range pods {
		addresses = append(addresses, corev1.EndpointAddress{
			IP:        "10.1.0.1",
			TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "web", Name: p},
		})
	}
	return []runtime.Object{
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "web"},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
			},
		},
		&corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "web"},
			Subsets: []corev1.EndpointSubset{{
				Addresses: addresses,
				Ports:     []corev1.EndpointPort{{Name: "http", Port: 80}},
			}},
		},
	}
}

func TestServiceForwardSessionView(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a", "frontend-b", "frontend-c")
	cluster := newFakeServiceCluster("ctx-a", frontendService("frontend-a", "frontend-b", "frontend-c")...)
	m := newTestPFManager(cluster, backendFactory(pods))

	pf, err := m.start(context.Background(), startRequest{
		Namespace: "web", Service: "frontend", ServicePort: 80,
	})
	require.NoError(t, err)
	assert.Equal(t, "service", pf.TargetKind)
	assert.Equal(t, "frontend", pf.Service)
	assert.Empty(t, pf.Pod, "a service forward has no single pod")
	assert.Equal(t, uint16(80), pf.RemotePort)
	assert.Equal(t, 3, pf.Backends)
	assert.NotZero(t, pf.LocalPort)

	// The list reports the *live* backend count, not the start-time snapshot.
	pods["frontend-b"].fwd.die()
	require.Eventually(t, func() bool {
		list := m.List()
		return len(list) == 1 && list[0].Backends == 2
	}, time.Second, 10*time.Millisecond, "the list must show the rotation shrinking")

	require.True(t, m.Stop(pf.ID))
	assert.Empty(t, m.List())
	for name, b := range pods {
		if name == "frontend-b" {
			continue // already gone on its own
		}
		assert.True(t, b.fwd.stopped.Load(), "stopping the session must stop %s's forward", name)
	}
}

func TestServiceForwardTornDownOnContextSwitch(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a", "frontend-b")
	cluster := newFakeServiceCluster("ctx-a", frontendService("frontend-a", "frontend-b")...)
	m := newTestPFManager(cluster, backendFactory(pods))

	_, err := m.start(context.Background(), startRequest{
		Namespace: "web", Service: "frontend", ServicePort: 80,
	})
	require.NoError(t, err)
	require.Len(t, m.List(), 1)

	// Switching context tears down the whole session — every per-pod forward.
	cluster.setContext("ctx-b")
	m.CloseOthers("ctx-b")
	assert.Empty(t, m.List())
	for name, b := range pods {
		assert.True(t, b.fwd.stopped.Load(), "context switch must stop %s's forward", name)
	}
}

func TestServiceForwardSessionEndsWhenAllBackendsDie(t *testing.T) {
	pods := newFakeBackendPods(t, "frontend-a", "frontend-b")
	cluster := newFakeServiceCluster("ctx-a", frontendService("frontend-a", "frontend-b")...)
	m := newTestPFManager(cluster, backendFactory(pods))

	_, err := m.start(context.Background(), startRequest{
		Namespace: "web", Service: "frontend", ServicePort: 80,
	})
	require.NoError(t, err)

	for _, b := range pods {
		b.fwd.die()
	}
	require.Eventually(t, func() bool { return len(m.List()) == 0 }, time.Second, 10*time.Millisecond,
		"losing every backend must drop the session from the list")
}

func TestServiceForwardStartErrors(t *testing.T) {
	tests := []struct {
		name    string
		objects []runtime.Object
		req     startRequest
		status  int
		code    string
	}{
		{
			name:    "service not found",
			objects: nil,
			req:     startRequest{Namespace: "web", Service: "frontend", ServicePort: 80},
			status:  http.StatusNotFound,
			code:    "not_found",
		},
		{
			name:    "port not on the service",
			objects: frontendService("frontend-a"),
			req:     startRequest{Namespace: "web", Service: "frontend", ServicePort: 8443},
			status:  http.StatusNotFound,
			code:    "not_found",
		},
		{
			name: "no ready endpoints",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "web"},
					Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80}}},
				},
			},
			req:    startRequest{Namespace: "web", Service: "frontend", ServicePort: 80},
			status: http.StatusConflict,
			code:   "no_ready_endpoints",
		},
		{
			name: "non-TCP port",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "web"},
					Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
						{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP},
					}},
				},
			},
			req:    startRequest{Namespace: "web", Service: "frontend", ServicePort: 53},
			status: http.StatusUnprocessableEntity,
			code:   "unsupported_protocol",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := newFakeServiceCluster("ctx-a", tc.objects...)
			m := newTestPFManager(cluster, backendFactory(newFakeBackendPods(t, "frontend-a")))

			body, err := json.Marshal(tc.req)
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			m.CreateHandler()(rec, httptest.NewRequest(http.MethodPost, "/portforwards", strings.NewReader(string(body))))

			assert.Equal(t, tc.status, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.code)
			assert.Empty(t, m.List(), "a failed start must register nothing")
		})
	}
}

func TestServiceForwardRejectsTooManyBackends(t *testing.T) {
	pods := make([]string, 0, maxServiceBackends+1)
	for i := 0; i <= maxServiceBackends; i++ {
		pods = append(pods, fmt.Sprintf("frontend-%02d", i))
	}
	cluster := newFakeServiceCluster("ctx-a", frontendService(pods...)...)
	// The factory must never be reached: the cap is checked before any forward.
	m := newTestPFManager(cluster, func(forwardTarget) (forwarder, error) {
		return nil, fmt.Errorf("factory must not be called past the backend cap")
	})

	_, err := m.start(context.Background(), startRequest{
		Namespace: "web", Service: "frontend", ServicePort: 80,
	})
	require.Error(t, err)
	var tooMany *TooManyBackendsError
	require.ErrorAs(t, err, &tooMany)
	status, code := forwardErrorStatus(err)
	assert.Equal(t, http.StatusUnprocessableEntity, status)
	assert.Equal(t, "too_many_backends", code)
}

func TestValidateStartRequestTargetDiscrimination(t *testing.T) {
	tests := []struct {
		name    string
		req     startRequest
		wantErr string
	}{
		{
			name: "pod target",
			req:  startRequest{Namespace: "web", Pod: "frontend-a", RemotePort: 80},
		},
		{
			name: "service target",
			req:  startRequest{Namespace: "web", Service: "frontend", ServicePort: 80},
		},
		{
			name:    "both targets",
			req:     startRequest{Namespace: "web", Pod: "frontend-a", RemotePort: 80, Service: "frontend", ServicePort: 80},
			wantErr: "exactly one of pod or service",
		},
		{
			name:    "neither target",
			req:     startRequest{Namespace: "web", RemotePort: 80},
			wantErr: "target is required",
		},
		{
			name:    "pod with servicePort",
			req:     startRequest{Namespace: "web", Pod: "frontend-a", RemotePort: 80, ServicePort: 80},
			wantErr: "servicePort belongs to a service target",
		},
		{
			name:    "service with remotePort",
			req:     startRequest{Namespace: "web", Service: "frontend", ServicePort: 80, RemotePort: 8080},
			wantErr: "remotePort belongs to a pod target",
		},
		{
			name:    "service without servicePort",
			req:     startRequest{Namespace: "web", Service: "frontend"},
			wantErr: "servicePort is required",
		},
		{
			name:    "pod without remotePort",
			req:     startRequest{Namespace: "web", Pod: "frontend-a"},
			wantErr: "remotePort is required",
		},
		{
			name:    "no namespace",
			req:     startRequest{Service: "frontend", ServicePort: 80},
			wantErr: "namespace is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStartRequest(tc.req)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
