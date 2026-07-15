package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// pfReadyTimeout bounds establishing a forward so a black-hole apiserver fails
// fast instead of parking the create request.
const pfReadyTimeout = 10 * time.Second

// pfLoopback is the only address a forward binds. The forwarded port lives
// inside the container; reaching it from the host requires publishing it
// (docker run -p). Binding all interfaces would expose cluster workloads on the
// host network — never the default (ADR-0005 posture).
const pfLoopback = "127.0.0.1"

// PortForwardCluster is the slice of the kube manager the port-forward manager
// needs: resolve the active context and build the clientset + rest.Config for it
// under one name. *kube.Manager satisfies it.
type PortForwardCluster interface {
	ActiveContextName() (string, error)
	ClientsetFor(name string) (kubernetes.Interface, error)
	RestConfigFor(name string) (*rest.Config, error)
}

// PortForward is the API view of one active forward.
type PortForward struct {
	ID         string    `json:"id"`
	Context    string    `json:"context"`
	Namespace  string    `json:"namespace"`
	Pod        string    `json:"pod"`
	LocalPort  uint16    `json:"localPort"`
	RemotePort uint16    `json:"remotePort"`
	StartedAt  time.Time `json:"startedAt"`
}

// forwardTarget is a resolved forward request handed to the forwarder factory.
type forwardTarget struct {
	restConfig *rest.Config
	clientset  kubernetes.Interface
	namespace  string
	pod        string
	localPort  uint16 // 0 = auto-assign a free local port
	remotePort uint16
}

// forwarder is a running port-forward. localPort reports the bound local port
// (known once ready); stop tears it down (idempotent); done fires when it exits
// on its own — pod deleted mid-forward, or a transport error.
type forwarder interface {
	localPort() uint16
	stop()
	done() <-chan struct{}
}

// forwarderFactory establishes a forward, blocking until it is ready (or fails).
// The default wires client-go's SPDY port-forward; tests inject a fake.
type forwarderFactory func(t forwardTarget) (forwarder, error)

// activeForward is a tracked forward plus its idempotent stop.
type activeForward struct {
	PortForward
	stop func()
}

// PortForwardManager owns every active forward for the process. Forwards are
// backend-managed sessions (ADR-0006): the browser starts/stops/lists them over
// a plain HTTP API and the byte-plumbing lives here. Forwards are torn down as a
// group on a context switch (those not on the new context) and on shutdown.
type PortForwardManager struct {
	cluster PortForwardCluster
	logger  *slog.Logger
	now     func() time.Time
	newID   func() string
	factory forwarderFactory
	idSeq   atomic.Uint64

	mu       sync.Mutex
	forwards map[string]*activeForward
}

// PFOption tunes a PortForwardManager.
type PFOption func(*PortForwardManager)

// WithClock overrides the start-time clock (tests).
func WithClock(now func() time.Time) PFOption { return func(m *PortForwardManager) { m.now = now } }

// withForwarderFactory overrides how forwards are established (tests only).
func withForwarderFactory(f forwarderFactory) PFOption {
	return func(m *PortForwardManager) { m.factory = f }
}

// withIDFunc overrides ID generation (tests only).
func withIDFunc(f func() string) PFOption { return func(m *PortForwardManager) { m.newID = f } }

// NewPortForwardManager builds a manager over the given cluster.
func NewPortForwardManager(cluster PortForwardCluster, logger *slog.Logger, opts ...PFOption) *PortForwardManager {
	m := &PortForwardManager{
		cluster:  cluster,
		logger:   logger,
		now:      time.Now,
		factory:  spdyForwarderFactory,
		forwards: make(map[string]*activeForward),
	}
	m.newID = func() string { return "pf-" + strconv.FormatUint(m.idSeq.Add(1), 10) }
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// startRequest is the create-forward request body.
type startRequest struct {
	Namespace  string `json:"namespace"`
	Pod        string `json:"pod"`
	RemotePort uint16 `json:"remotePort"`
	LocalPort  uint16 `json:"localPort"` // 0 = auto-assign
}

// start resolves the active context, establishes the forward and registers it.
// A forward that later dies on its own is dropped from the registry by a watcher.
func (m *PortForwardManager) start(req startRequest) (PortForward, error) {
	ctxName, err := m.cluster.ActiveContextName()
	if err != nil {
		return PortForward{}, err
	}
	clientset, err := m.cluster.ClientsetFor(ctxName)
	if err != nil {
		return PortForward{}, err
	}
	restCfg, err := m.cluster.RestConfigFor(ctxName)
	if err != nil {
		return PortForward{}, err
	}

	fwd, err := m.factory(forwardTarget{
		restConfig: restCfg,
		clientset:  clientset,
		namespace:  req.Namespace,
		pod:        req.Pod,
		localPort:  req.LocalPort,
		remotePort: req.RemotePort,
	})
	if err != nil {
		return PortForward{}, err
	}

	id := m.newID()
	pf := PortForward{
		ID:         id,
		Context:    ctxName,
		Namespace:  req.Namespace,
		Pod:        req.Pod,
		LocalPort:  fwd.localPort(),
		RemotePort: req.RemotePort,
		StartedAt:  m.now(),
	}
	m.mu.Lock()
	m.forwards[id] = &activeForward{PortForward: pf, stop: fwd.stop}
	m.mu.Unlock()

	// Establishing the forward can block for seconds; a context switch during
	// that window runs CloseOthers before this forward was registered, so it
	// would miss it and the forward would outlive its context. Reconcile: if the
	// active context has moved on, tear this forward down now. remove() is
	// idempotent, so a CloseOthers that already reaped it just makes this a no-op.
	if current, cerr := m.cluster.ActiveContextName(); cerr != nil || current != ctxName {
		if m.remove(id) {
			fwd.stop()
		}
		return PortForward{}, fmt.Errorf("active context changed while establishing forward to %s/%s", req.Namespace, req.Pod)
	}

	// A forward exiting on its own (pod deleted mid-forward) is removed so the
	// list never shows a dead forward. A deliberate Stop closes the same done
	// channel; remove is idempotent, so the watcher is harmless either way.
	go func() {
		<-fwd.done()
		if m.remove(id) {
			m.logger.Info("port-forward closed", "id", id, "pod", req.Pod, "namespace", req.Namespace)
		}
	}()
	return pf, nil
}

// remove drops a forward from the registry, reporting whether it was present.
func (m *PortForwardManager) remove(id string) bool {
	m.mu.Lock()
	_, ok := m.forwards[id]
	delete(m.forwards, id)
	m.mu.Unlock()
	return ok
}

// Stop tears down a forward by id, reporting whether it existed. Idempotent: a
// second Stop (or a Stop racing the dead-watcher) is a no-op.
func (m *PortForwardManager) Stop(id string) bool {
	m.mu.Lock()
	af, ok := m.forwards[id]
	if ok {
		delete(m.forwards, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	af.stop()
	return true
}

// List returns every active forward, oldest first. Because forwards are torn
// down on a context switch, the list only ever holds the active context's
// forwards; the Context field is included so the UI can still show it.
func (m *PortForwardManager) List() []PortForward {
	m.mu.Lock()
	out := make([]PortForward, 0, len(m.forwards))
	for _, af := range m.forwards {
		out = append(out, af.PortForward)
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

// CloseOthers stops every forward not on the current context — the context-switch
// teardown.
func (m *PortForwardManager) CloseOthers(current string) {
	m.mu.Lock()
	var doomed []*activeForward
	for id, af := range m.forwards {
		if af.Context != current {
			doomed = append(doomed, af)
			delete(m.forwards, id)
		}
	}
	m.mu.Unlock()
	for _, af := range doomed {
		af.stop()
	}
}

// CloseAll stops every forward — the shutdown teardown.
func (m *PortForwardManager) CloseAll() {
	m.mu.Lock()
	doomed := make([]*activeForward, 0, len(m.forwards))
	for id, af := range m.forwards {
		doomed = append(doomed, af)
		delete(m.forwards, id)
	}
	m.mu.Unlock()
	for _, af := range doomed {
		af.stop()
	}
}

// CreateHandler serves POST /api/v1/portforwards: start a forward from a pod
// port to a backend loopback listener. It sits behind the read-only guard, so a
// read-only server rejects it with 403 before this runs (ADR-0005).
func (m *PortForwardManager) CreateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req startRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeStreamError(w, m.logger, http.StatusBadRequest, "invalid_request",
				fmt.Sprintf("invalid request body: %v", err))
			return
		}
		if err := validateStartRequest(req); err != nil {
			writeStreamError(w, m.logger, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		pf, err := m.start(req)
		if err != nil {
			status, code := forwardErrorStatus(err)
			writeStreamError(w, m.logger, status, code,
				fmt.Sprintf("starting forward to %s/%s: %v", req.Namespace, req.Pod, err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(pf); err != nil {
			m.logger.Error("encoding port-forward response", "error", err)
		}
	}
}

// ListHandler serves GET /api/v1/portforwards: the active-forwards list. A read,
// so it is not gated by read-only mode.
func (m *PortForwardManager) ListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"items": m.List()}); err != nil {
			m.logger.Error("encoding port-forward list", "error", err)
		}
	}
}

// DeleteHandler serves DELETE /api/v1/portforwards/{id}: stop a forward. Stopping
// only ends a backend-local listener the user already started, so it is not a
// cluster mutation and is not gated by read-only mode.
func (m *PortForwardManager) DeleteHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !m.Stop(id) {
			writeStreamError(w, m.logger, http.StatusNotFound, "not_found",
				fmt.Sprintf("no active forward %q", id))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// validateStartRequest checks the create-forward parameters before any cluster
// call, so bad input is a fast 400 rather than a dial failure.
func validateStartRequest(req startRequest) error {
	if req.Namespace == "" {
		return errors.New("namespace is required")
	}
	if req.Pod == "" {
		return errors.New("pod is required")
	}
	if req.RemotePort == 0 {
		return errors.New("remotePort is required and must be 1-65535")
	}
	return nil
}

// forwardErrorStatus maps a start failure to an HTTP status + code, extending the
// read taxonomy with the port-in-use case (a local listener conflict, not an
// apiserver error).
func forwardErrorStatus(err error) (int, string) {
	switch {
	case apierrors.IsNotFound(err):
		return http.StatusNotFound, "not_found"
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return http.StatusForbidden, "forbidden"
	case strings.Contains(err.Error(), "address already in use"):
		return http.StatusConflict, "port_in_use"
	default:
		return http.StatusBadGateway, "cluster_unreachable"
	}
}

// spdyForwarderFactory establishes a real SPDY port-forward, blocking until it is
// ready. All human-readable output is discarded — forwarded traffic and its
// metadata are never logged.
func spdyForwarderFactory(t forwardTarget) (forwarder, error) {
	transport, upgrader, err := spdy.RoundTripperFor(t.restConfig)
	if err != nil {
		return nil, fmt.Errorf("building spdy transport: %w", err)
	}
	req := t.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(t.namespace).
		Name(t.pod).
		SubResource("portforward")
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	ports := []string{fmt.Sprintf("%d:%d", t.localPort, t.remotePort)}

	pf, err := portforward.NewOnAddresses(dialer, []string{pfLoopback}, ports, stopCh, readyCh, io.Discard, io.Discard)
	if err != nil {
		return nil, err
	}

	doneCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		defer close(doneCh)
		if err := pf.ForwardPorts(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-readyCh:
		local, err := boundLocalPort(pf)
		if err != nil {
			close(stopCh)
			return nil, err
		}
		return &spdyForwarder{stopCh: stopCh, doneCh: doneCh, local: local}, nil
	case err := <-errCh:
		return nil, err
	case <-doneCh:
		// ForwardPorts sends to errCh before it closes doneCh, so if it failed
		// (e.g. "address already in use") the error is already buffered — prefer
		// it over the generic message so classification (port_in_use) survives.
		select {
		case err := <-errCh:
			return nil, err
		default:
			return nil, errors.New("port-forward closed before becoming ready")
		}
	case <-time.After(pfReadyTimeout):
		close(stopCh)
		return nil, errors.New("timed out establishing port-forward")
	}
}

// boundLocalPort reads the actually-bound local port after readiness (the
// requested port, or the OS-assigned one when 0 was requested).
func boundLocalPort(pf *portforward.PortForwarder) (uint16, error) {
	ports, err := pf.GetPorts()
	if err != nil {
		return 0, fmt.Errorf("reading forwarded ports: %w", err)
	}
	if len(ports) == 0 {
		return 0, errors.New("port-forward reported no bound ports")
	}
	return ports[0].Local, nil
}

// spdyForwarder is the real forwarder backed by client-go's PortForwarder.
type spdyForwarder struct {
	stopCh   chan struct{}
	doneCh   chan struct{}
	local    uint16
	stopOnce sync.Once
}

func (f *spdyForwarder) localPort() uint16     { return f.local }
func (f *spdyForwarder) done() <-chan struct{} { return f.doneCh }
func (f *spdyForwarder) stop()                 { f.stopOnce.Do(func() { close(f.stopCh) }) }
