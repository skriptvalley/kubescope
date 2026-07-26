package stream

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skriptvalley/kubescope/internal/resources"
)

// Service port-forward (FB-13, ADR-0006 addendum). A pod forward is 1:1 — one
// SPDY tunnel to one pod. A service forward is 1:N: one SPDY tunnel per ready
// endpoint pod, fronted by a single loopback listener that hands each *new TCP
// connection* to the next live backend. That is the same granularity ClusterIP
// gives (kube-proxy balances connections, not requests), so a caller sees the
// service's real behavior rather than an L7 proxy's.
//
// Backends are snapshotted when the forward starts; EndpointSlice churn after
// that is not tracked (recorded as a follow-up in the ADR). A backend that dies
// drops out of the rotation, and the session ends when the last one is gone —
// the same lifecycle a pod forward has when its pod is deleted.

// maxServiceBackends bounds one service forward's fan-out. Each backend costs a
// SPDY tunnel plus a loopback listener, so an unbounded rotation would let one
// API call open hundreds of connections against the apiserver. Exceeding it is a
// hard, classified failure rather than a silent truncation to a partial
// rotation, which would balance over a subset while claiming to front the
// Service.
const maxServiceBackends = 64

// Accept-loop backoff: a transient accept failure is retried, a persistently
// broken listener ends the session.
const (
	acceptRetryDelay  = 5 * time.Millisecond
	acceptMaxDelay    = time.Second
	acceptMaxFailures = 10
)

// TooManyBackendsError reports a Service with more ready endpoints than one
// load-balanced forward will fan out to.
type TooManyBackendsError struct {
	Namespace string
	Service   string
	Ready     int
	Max       int
}

func (e *TooManyBackendsError) Error() string {
	return fmt.Sprintf("service %s/%s has %d ready endpoints; a load-balanced forward supports at most %d",
		e.Namespace, e.Service, e.Ready, e.Max)
}

// serviceBackend is one per-pod forward in the rotation. Only dead flips after
// construction, so the surrounding slice needs no lock.
type serviceBackend struct {
	pod  string
	addr string // 127.0.0.1:<that forward's local port>
	fwd  forwarder
	dead atomic.Bool
}

// serviceForwarder is the load-balancing front for a set of per-pod forwards. It
// satisfies forwarder, so the manager tracks, stops and reaps a service session
// exactly like a pod one.
type serviceForwarder struct {
	listener net.Listener
	local    uint16
	backends []*serviceBackend // immutable after construction

	mu     sync.Mutex
	next   int                   // round-robin cursor
	conns  map[net.Conn]struct{} // in-flight client connections, closed by stop
	closed bool

	doneCh   chan struct{}
	stopOnce sync.Once
}

// newServiceForwarder establishes one forward per backend pod and binds the
// public loopback listener in front of them. Any failure tears down whatever was
// already established, so a failed start leaves nothing running.
func newServiceForwarder(
	factory forwarderFactory,
	base forwardTarget,
	backends []resources.ServiceBackend,
	localPort uint16,
) (*serviceForwarder, error) {
	if len(backends) == 0 {
		return nil, errors.New("no ready endpoints to forward to")
	}

	established, err := establishBackends(factory, base, backends)
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(pfLoopback, strconv.Itoa(int(localPort))))
	if err != nil {
		stopBackends(established)
		// Keep the underlying message intact: "address already in use" is what
		// classifies this as port_in_use rather than a cluster failure.
		return nil, fmt.Errorf("binding local listener: %w", err)
	}
	local, err := listenerPort(listener)
	if err != nil {
		_ = listener.Close()
		stopBackends(established)
		return nil, err
	}

	s := &serviceForwarder{
		listener: listener,
		local:    local,
		backends: established,
		conns:    make(map[net.Conn]struct{}),
		doneCh:   make(chan struct{}),
	}
	go s.acceptLoop()
	for _, b := range s.backends {
		go s.watchBackend(b)
	}
	return s, nil
}

// establishBackends opens the per-pod forwards concurrently — sequential
// establishment would multiply the per-forward readiness timeout by the endpoint
// count and park the create request. On any failure every successful forward is
// stopped and the first failure (by backend order, so it is deterministic) is
// returned.
func establishBackends(
	factory forwarderFactory,
	base forwardTarget,
	backends []resources.ServiceBackend,
) ([]*serviceBackend, error) {
	out := make([]*serviceBackend, len(backends))
	errs := make([]error, len(backends))

	var wg sync.WaitGroup
	for i, b := range backends {
		wg.Add(1)
		go func(i int, b resources.ServiceBackend) {
			defer wg.Done()
			target := base
			target.namespace = b.Namespace
			target.pod = b.Pod
			target.remotePort = b.Port
			target.localPort = 0 // every per-pod leg gets an ephemeral port
			fwd, err := factory(target)
			if err != nil {
				errs[i] = fmt.Errorf("forwarding to %s/%s: %w", b.Namespace, b.Pod, err)
				return
			}
			out[i] = &serviceBackend{
				pod:  b.Pod,
				addr: net.JoinHostPort(pfLoopback, strconv.Itoa(int(fwd.localPort()))),
				fwd:  fwd,
			}
		}(i, b)
	}
	wg.Wait()

	for _, err := range errs {
		if err == nil {
			continue
		}
		stopBackends(out)
		return nil, err
	}
	return out, nil
}

// stopBackends tears down every established forward in a partially-built set.
func stopBackends(backends []*serviceBackend) {
	for _, b := range backends {
		if b != nil {
			b.fwd.stop()
		}
	}
}

// listenerPort reads the actually-bound local port (the requested one, or the
// OS-assigned one when 0 was requested).
func listenerPort(l net.Listener) (uint16, error) {
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("local listener reported no TCP address")
	}
	return uint16(addr.Port), nil
}

func (s *serviceForwarder) localPort() uint16     { return s.local }
func (s *serviceForwarder) done() <-chan struct{} { return s.doneCh }

// stop tears the whole session down: the public listener, every in-flight
// connection, and every per-pod forward. Idempotent.
func (s *serviceForwarder) stop() {
	s.stopOnce.Do(func() {
		_ = s.listener.Close()

		s.mu.Lock()
		s.closed = true
		conns := make([]net.Conn, 0, len(s.conns))
		for c := range s.conns {
			conns = append(conns, c)
		}
		s.conns = nil
		s.mu.Unlock()

		for _, c := range conns {
			_ = c.Close()
		}
		stopBackends(s.backends)
		close(s.doneCh)
	})
}

// backendCount reports how many backends are still live — the number the API
// view surfaces so the UI shows a rotation shrinking as pods go away.
func (s *serviceForwarder) backendCount() int {
	n := 0
	for _, b := range s.backends {
		if !b.dead.Load() {
			n++
		}
	}
	return n
}

// acceptLoop hands every new connection to the next live backend.
//
// A closed listener is the deliberate teardown and simply ends the loop. Any
// other accept failure is retried with backoff rather than treated as fatal:
// fd exhaustion (EMFILE) surfaces here, and one transient blip must not silently
// destroy a working forward — the same reason net/http's Serve loop backs off
// instead of returning. A listener that keeps failing is genuinely unusable, so
// after acceptMaxFailures consecutive errors the session ends rather than
// lingering in the active list unable to serve.
func (s *serviceForwarder) acceptLoop() {
	delay := acceptRetryDelay
	failures := 0
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if failures++; failures > acceptMaxFailures {
				s.stop()
				return
			}
			time.Sleep(delay)
			if delay *= 2; delay > acceptMaxDelay {
				delay = acceptMaxDelay
			}
			continue
		}
		failures, delay = 0, acceptRetryDelay
		go s.handle(conn)
	}
}

// watchBackend drops a backend from the rotation when its forward exits on its
// own (pod deleted mid-forward). The session survives while at least one backend
// is live; when the last one goes, it closes the way a dead pod forward does.
func (s *serviceForwarder) watchBackend(b *serviceBackend) {
	<-b.fwd.done()
	b.dead.Store(true)
	if s.backendCount() == 0 {
		s.stop()
	}
}

// handle splices one client connection to the next live backend. A backend that
// is mid-teardown may refuse the dial before its done channel fires, so the
// rotation is retried up to once per backend before the connection is dropped.
//
// The retry does not cover a backend whose pod has just been deleted: its local
// listener is still bound, so the dial succeeds and the SPDY stream fails after
// the request bytes are already gone. That connection fails, and the failure is
// what makes the forward exit and drop out of the rotation — the same detection
// window a pod forward has (ADR-0006 addendum). Replaying it would mean
// buffering request bytes, which the per-connection design exists to avoid.
func (s *serviceForwarder) handle(client net.Conn) {
	if !s.track(client) {
		_ = client.Close()
		return
	}
	defer s.untrack(client)
	defer func() { _ = client.Close() }()

	for attempt := 0; attempt < len(s.backends); attempt++ {
		b := s.pick()
		if b == nil {
			return // no live backend: drop it, as a Service with no endpoints would
		}
		upstream, err := net.Dial("tcp", b.addr)
		if err != nil {
			continue
		}
		// Track the upstream half too, so stop() owns teardown of both ends
		// rather than depending on the backend closing its side first.
		if !s.track(upstream) {
			_ = upstream.Close()
			return
		}
		defer s.untrack(upstream)
		splice(client, upstream)
		return
	}
}

// pick returns the next live backend in round-robin order, or nil when the
// rotation is empty.
func (s *serviceForwarder) pick() *serviceBackend {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.backends)
	if n == 0 {
		return nil
	}
	for i := 0; i < n; i++ {
		b := s.backends[s.next%n]
		s.next = (s.next + 1) % n
		if !b.dead.Load() {
			return b
		}
	}
	return nil
}

// track registers an in-flight connection so stop can close it, reporting false
// if the session already stopped (the connection raced the teardown).
func (s *serviceForwarder) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *serviceForwarder) untrack(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
}

// splice joins a client connection to its chosen backend, copying in both
// directions until either side ends. Bytes are moved, never inspected, buffered
// for inspection or logged — the io.Discard posture of the pod forward extends
// to the balancer.
func splice(client, upstream net.Conn) {
	defer func() { _ = upstream.Close() }()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(upstream, client)
		closeWrite(upstream)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, upstream)
		closeWrite(client)
	}()
	wg.Wait()
}

// closeWrite half-closes one direction so the peer sees EOF while the other
// direction keeps draining — what a transparent TCP proxy must preserve for
// protocols that signal end-of-request by shutdown.
func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}
