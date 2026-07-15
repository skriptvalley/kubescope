package stream

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// switchableCluster lets a test flip the active context mid-stream to exercise
// the context-switch teardown path.
type switchableCluster struct {
	dyn dynamic.Interface
	mu  sync.Mutex
	ctx string
}

func (c *switchableCluster) ActiveContextName() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx, nil
}
func (c *switchableCluster) DynamicFor(string) (dynamic.Interface, error) { return c.dyn, nil }
func (c *switchableCluster) switchTo(name string) {
	c.mu.Lock()
	c.ctx = name
	c.mu.Unlock()
}

type sseFrame struct {
	event string
	data  string
}

// readSSE parses the SSE stream into frames on a channel until the body closes.
func readSSE(r io.Reader, out chan<- sseFrame) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "" || strings.HasPrefix(line, ":"):
			// frame boundary or heartbeat comment
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			out <- sseFrame{event: event, data: strings.TrimPrefix(line, "data: ")}
			event = ""
		}
	}
	close(out)
}

func nextFrame(t *testing.T, frames <-chan sseFrame, timeout time.Duration) (sseFrame, bool) {
	t.Helper()
	select {
	case f, ok := <-frames:
		return f, ok
	case <-time.After(timeout):
		t.Fatal("timed out waiting for SSE frame")
		return sseFrame{}, false
	}
}

func TestStreamHandlerDeliversWatchEvents(t *testing.T) {
	client := newFakeClient(t)
	createPod(t, client, "default", "seed")
	cluster := &switchableCluster{dyn: client, ctx: "ctx-a"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := NewHub(cluster, nameShaper, logger)

	r := chi.NewRouter()
	r.Get("/stream/resources/{group}/{version}/{resource}", StreamHandler(hub, logger, WithHeartbeat(100*time.Millisecond)))
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/resources/core/v1/pods?namespace=default", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	frames := make(chan sseFrame, 32)
	go readSSE(resp.Body, frames)

	// Initial snapshot: the seed pod arrives as an add.
	f, _ := nextFrame(t, frames, 3*time.Second)
	assert.Contains(t, f.data, `"type":"add"`)
	assert.Contains(t, f.data, `"row":"seed"`)

	// A live create is delivered.
	createPod(t, client, "default", "live")
	f, _ = nextFrame(t, frames, 3*time.Second)
	assert.Contains(t, f.data, `"type":"add"`)
	assert.Contains(t, f.data, `"row":"live"`)

	// Switching context closes the stream bound to the previous one.
	cluster.switchTo("ctx-b")
	require.Eventually(t, func() bool {
		select {
		case _, ok := <-frames:
			return !ok // channel closed => body ended => stream closed
		default:
			return false
		}
	}, 3*time.Second, 50*time.Millisecond, "stream must close after context switch")
}

func TestStreamHandlerEmitsHeartbeat(t *testing.T) {
	client := newFakeClient(t) // no objects → an idle stream that only heartbeats
	cluster := &switchableCluster{dyn: client, ctx: "ctx-a"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := NewHub(cluster, nameShaper, logger)

	r := chi.NewRouter()
	r.Get("/stream/resources/{group}/{version}/{resource}", StreamHandler(hub, logger, WithHeartbeat(40*time.Millisecond)))
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/resources/core/v1/pods", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read raw lines (readSSE drops comments) and assert a heartbeat comment
	// arrives on an idle stream — the AC-4.1 keep-alive.
	comments := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), ":") {
				comments <- sc.Text()
			}
		}
		close(comments)
	}()

	select {
	case line := <-comments:
		assert.Contains(t, line, "ping")
	case <-time.After(3 * time.Second):
		t.Fatal("no heartbeat comment received on an idle stream")
	}
}

func TestStreamHandlerResyncOnBufferOverflow(t *testing.T) {
	client := newFakeClient(t)
	cluster := &switchableCluster{dyn: client, ctx: "ctx-a"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := NewHub(cluster, nameShaper, logger, WithEventBuffer(1))

	r := chi.NewRouter()
	r.Get("/stream/resources/{group}/{version}/{resource}", StreamHandler(hub, logger, WithHeartbeat(80*time.Millisecond)))
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/resources/core/v1/pods", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	frames := make(chan sseFrame, 128)
	go readSSE(resp.Body, frames)

	// Flood so some events overflow the size-1 buffer; the handler must emit a
	// resync so the client refetches a clean baseline.
	for i := 0; i < 40; i++ {
		createPod(t, client, "default", string(rune('a'+i%26))+string(rune('0'+i/26)))
	}

	sawResync := false
	deadline := time.After(4 * time.Second)
	for !sawResync {
		select {
		case f := <-frames:
			if strings.Contains(f.data, `"type":"resync"`) {
				sawResync = true
			}
		case <-deadline:
			t.Fatal("expected a resync frame after buffer overflow")
		}
	}
}

func TestLogsHandlerStreamsAndCloses(t *testing.T) {
	cs := fake.NewClientset()
	cluster := &fakeLogCluster{cs: cs}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	r := chi.NewRouter()
	r.Get("/stream/pods/{namespace}/{name}/logs", LogsHandler(cluster, logger, WithHeartbeat(time.Second)))
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/pods/default/web-1/logs?container=web", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	frames := make(chan sseFrame, 8)
	go readSSE(resp.Body, frames)

	// The fake clientset streams a fixed body then EOF.
	line, _ := nextFrame(t, frames, 3*time.Second)
	assert.Equal(t, "", line.event)
	assert.Contains(t, line.data, `"line":"fake logs"`)

	closed, _ := nextFrame(t, frames, 3*time.Second)
	assert.Equal(t, "closed", closed.event)
	assert.Contains(t, closed.data, `"reason":"eof"`)
}

func TestLogsHandlerRejectsBadTailLines(t *testing.T) {
	cs := fake.NewClientset()
	cluster := &fakeLogCluster{cs: cs}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	r := chi.NewRouter()
	r.Get("/stream/pods/{namespace}/{name}/logs", LogsHandler(cluster, logger))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream/pods/default/web-1/logs?tailLines=-5", nil))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_request")
}

type fakeLogCluster struct{ cs kubernetes.Interface }

func (f *fakeLogCluster) ActiveContextName() (string, error) { return "ctx", nil }
func (f *fakeLogCluster) ClientsetFor(string) (kubernetes.Interface, error) {
	return f.cs, nil
}
