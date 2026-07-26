package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// fakeForwarder is a controllable stand-in for a running port-forward: stop()
// mimics a deliberate teardown, die() mimics a self-exit (pod deleted); both
// close the done channel exactly once.
type fakeForwarder struct {
	local   uint16
	doneCh  chan struct{}
	once    sync.Once
	stopped atomic.Bool
}

func newFakeForwarder(local uint16) *fakeForwarder {
	return &fakeForwarder{local: local, doneCh: make(chan struct{})}
}

func (f *fakeForwarder) localPort() uint16     { return f.local }
func (f *fakeForwarder) done() <-chan struct{} { return f.doneCh }
func (f *fakeForwarder) stop()                 { f.once.Do(func() { f.stopped.Store(true); close(f.doneCh) }) }
func (f *fakeForwarder) die()                  { f.once.Do(func() { close(f.doneCh) }) }

func recordingFactory(out *[]*fakeForwarder, local uint16) forwarderFactory {
	return func(forwardTarget) (forwarder, error) {
		f := newFakeForwarder(local)
		*out = append(*out, f)
		return f, nil
	}
}

// fakePFCluster resolves to a settable active context; the clients are unused by
// the fake factory but keep the resolution path honest.
type fakePFCluster struct {
	mu  sync.Mutex
	ctx string
}

func (c *fakePFCluster) setContext(name string) {
	c.mu.Lock()
	c.ctx = name
	c.mu.Unlock()
}
func (c *fakePFCluster) ActiveContextName() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx, nil
}
func (c *fakePFCluster) ClientsetFor(string) (kubernetes.Interface, error) {
	return fake.NewClientset(), nil
}
func (c *fakePFCluster) RestConfigFor(string) (*rest.Config, error) {
	return &rest.Config{Host: "https://example.test"}, nil
}

func newTestPFManager(cluster PortForwardCluster, factory forwarderFactory) *PortForwardManager {
	var seq int
	return NewPortForwardManager(cluster, discardLogger(),
		withForwarderFactory(factory),
		withIDFunc(func() string { seq++; return fmt.Sprintf("pf-%d", seq) }),
		WithClock(func() time.Time { return time.Unix(0, 0) }),
	)
}

func TestPortForwardLifecycle(t *testing.T) {
	var created []*fakeForwarder
	m := newTestPFManager(&fakePFCluster{ctx: "ctx-a"}, recordingFactory(&created, 15000))

	pf, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "nginx", RemotePort: 80})
	require.NoError(t, err)
	assert.Equal(t, "default", pf.Namespace)
	assert.Equal(t, "nginx", pf.Pod)
	assert.Equal(t, uint16(80), pf.RemotePort)
	assert.Equal(t, uint16(15000), pf.LocalPort)
	assert.Equal(t, "ctx-a", pf.Context)
	assert.NotEmpty(t, pf.ID)
	require.Len(t, m.List(), 1)

	assert.True(t, m.Stop(pf.ID))
	assert.False(t, m.Stop(pf.ID), "double-stop must be a no-op, not a panic")
	assert.Empty(t, m.List())
	assert.True(t, created[0].stopped.Load(), "Stop must tear the forwarder down")
}

func TestPortForwardAutoRemovesDeadForward(t *testing.T) {
	var created []*fakeForwarder
	m := newTestPFManager(&fakePFCluster{ctx: "ctx-a"}, recordingFactory(&created, 15000))

	_, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "nginx", RemotePort: 80})
	require.NoError(t, err)
	require.Len(t, m.List(), 1)

	// The forward dies on its own (pod deleted mid-forward): the watcher drops it.
	created[0].die()
	require.Eventually(t, func() bool { return len(m.List()) == 0 }, time.Second, 10*time.Millisecond,
		"a self-closed forward must be removed from the list")
}

func TestPortForwardReconcilesContextSwitchDuringEstablish(t *testing.T) {
	// A context switch during the (blocking) establish would run CloseOthers
	// before this forward was registered; the post-establish reconcile must tear
	// it down rather than let it outlive its context.
	cluster := &fakePFCluster{ctx: "ctx-a"}
	var created []*fakeForwarder
	// The factory simulates the switch happening while it establishes.
	factory := func(t forwardTarget) (forwarder, error) {
		cluster.setContext("ctx-b")
		f := newFakeForwarder(15000)
		created = append(created, f)
		return f, nil
	}
	m := newTestPFManager(cluster, factory)

	_, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "nginx", RemotePort: 80})
	require.Error(t, err, "start must fail when the context moved on during establish")
	assert.Empty(t, m.List(), "the stale-context forward must not be registered")
	require.Len(t, created, 1)
	assert.True(t, created[0].stopped.Load(), "the stale-context forward must be stopped")
}

func TestPortForwardCloseOthers(t *testing.T) {
	var created []*fakeForwarder
	cluster := &fakePFCluster{ctx: "ctx-a"}
	m := newTestPFManager(cluster, recordingFactory(&created, 15000))

	a, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "a", RemotePort: 80})
	require.NoError(t, err)
	cluster.setContext("ctx-b")
	b, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "b", RemotePort: 80})
	require.NoError(t, err)
	require.Len(t, m.List(), 2)

	// Switching to ctx-b tears down the ctx-a forward, keeps the ctx-b one.
	m.CloseOthers("ctx-b")
	list := m.List()
	require.Len(t, list, 1)
	assert.Equal(t, b.ID, list[0].ID)
	assert.True(t, created[0].stopped.Load(), "ctx-a forward must be stopped")
	assert.False(t, created[1].stopped.Load(), "ctx-b forward must survive")
	_ = a
}

func TestPortForwardCloseAll(t *testing.T) {
	var created []*fakeForwarder
	m := newTestPFManager(&fakePFCluster{ctx: "ctx-a"}, recordingFactory(&created, 15000))
	_, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "a", RemotePort: 80})
	require.NoError(t, err)
	_, err = m.start(context.Background(), startRequest{Namespace: "default", Pod: "b", RemotePort: 81})
	require.NoError(t, err)
	require.Len(t, m.List(), 2)

	m.CloseAll()
	assert.Empty(t, m.List())
	for _, f := range created {
		assert.True(t, f.stopped.Load())
	}
}

func TestCreateHandlerValidation(t *testing.T) {
	m := newTestPFManager(&fakePFCluster{ctx: "ctx-a"}, recordingFactory(new([]*fakeForwarder), 15000))
	cases := []struct{ name, body string }{
		{"missing namespace", `{"pod":"nginx","remotePort":80}`},
		{"missing pod", `{"namespace":"default","remotePort":80}`},
		{"missing remotePort", `{"namespace":"default","pod":"nginx"}`},
		{"malformed json", `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m.CreateHandler()(rec, httptest.NewRequest(http.MethodPost, "/portforwards", strings.NewReader(tc.body)))
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), "invalid_request")
		})
	}
}

func TestCreateHandlerSuccess(t *testing.T) {
	var created []*fakeForwarder
	m := newTestPFManager(&fakePFCluster{ctx: "ctx-a"}, recordingFactory(&created, 15000))

	rec := httptest.NewRecorder()
	body := `{"namespace":"default","pod":"nginx","remotePort":80}`
	m.CreateHandler()(rec, httptest.NewRequest(http.MethodPost, "/portforwards", strings.NewReader(body)))

	require.Equal(t, http.StatusCreated, rec.Code)
	var pf PortForward
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pf))
	assert.Equal(t, "nginx", pf.Pod)
	assert.Equal(t, uint16(15000), pf.LocalPort)
	assert.Len(t, m.List(), 1)
}

func TestDeleteHandler(t *testing.T) {
	var created []*fakeForwarder
	m := newTestPFManager(&fakePFCluster{ctx: "ctx-a"}, recordingFactory(&created, 15000))
	pf, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "nginx", RemotePort: 80})
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Delete("/portforwards/{id}", m.DeleteHandler())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/portforwards/"+pf.ID, nil))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/portforwards/unknown", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "not_found")
}

func TestListHandler(t *testing.T) {
	var created []*fakeForwarder
	m := newTestPFManager(&fakePFCluster{ctx: "ctx-a"}, recordingFactory(&created, 15000))
	_, err := m.start(context.Background(), startRequest{Namespace: "default", Pod: "a", RemotePort: 80})
	require.NoError(t, err)
	_, err = m.start(context.Background(), startRequest{Namespace: "default", Pod: "b", RemotePort: 81})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	m.ListHandler()(rec, httptest.NewRequest(http.MethodGet, "/portforwards", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Items []PortForward `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 2)
}
