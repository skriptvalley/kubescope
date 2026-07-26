package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

var testPodsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

func TestDrainGuardCancelsRequestContext(t *testing.T) {
	drain := make(chan struct{})
	done := make(chan struct{})
	h := drainGuard(drain)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(done)
	}))

	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stream", nil))

	select {
	case <-done:
		t.Fatal("handler unwound before shutdown started")
	case <-time.After(50 * time.Millisecond):
	}

	close(drain)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not unwind after the drain signal")
	}
}

// A nil drain channel is the router-only-test wiring: the guard must be inert,
// never cancelling a request that would otherwise run to completion.
func TestDrainGuardNilIsInert(t *testing.T) {
	var gotErr error
	served := false
	h := drainGuard(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		served = true
		gotErr = r.Context().Err()
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stream", nil))

	assert.True(t, served, "handler should still be reached")
	assert.NoError(t, gotErr, "request context should not be cancelled")
}

// Regression (FB-16): http.Server.Shutdown waits for active requests, so an SSE
// stream — a request that ends only when the client leaves — used to hold the
// whole shutdown timeout and then fail with a deadline error. The unguarded
// case is the pre-fix behavior and pins the regression.
func TestShutdownDoesNotWaitForGuardedStreams(t *testing.T) {
	tests := []struct {
		name    string
		guarded bool
		timeout time.Duration
		wantErr error
	}{
		{name: "guarded stream unwinds", guarded: true, timeout: 5 * time.Second},
		{name: "unguarded stream burns the deadline", guarded: false, timeout: 300 * time.Millisecond, wantErr: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drain := make(chan struct{})
			started := make(chan struct{})
			var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush() // headers out, so the client's Do returns
				close(started)
				<-r.Context().Done() // a stream: never ends on its own
			})
			if tt.guarded {
				handler = drainGuard(drain)(handler)
			}

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
			srv.RegisterOnShutdown(sync.OnceFunc(func() { close(drain) }))
			go func() { _ = srv.Serve(ln) }()
			t.Cleanup(func() { _ = srv.Close() })

			resp, err := http.Get("http://" + ln.Addr().String() + "/stream")
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			<-started

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()
			start := time.Now()
			err = srv.Shutdown(ctx)
			elapsed := time.Since(start)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.GreaterOrEqual(t, elapsed, tt.timeout, "an unguarded stream should hold shutdown to its deadline")
				return
			}
			require.NoError(t, err)
			assert.Less(t, elapsed, tt.timeout/2, "shutdown should not wait on a stream that was told to drain")
		})
	}
}

// The guard is only useful if the streaming routes actually carry it, so this
// drives the real router: an open watch stream must end when drain fires.
func TestStreamRouteUnwindsOnDrain(t *testing.T) {
	drain := make(chan struct{})
	provider := &fakeProvider{
		clientset: fake.NewClientset(),
		dynamic: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
			runtime.NewScheme(),
			map[schema.GroupVersionResource]string{testPodsGVR: "PodList"},
		),
	}
	srv := httptest.NewServer(New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   provider,
		Stream: provider,
		Drain:  drain,
		Dist:   spaFixture(),
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/stream/resources/core/v1/pods")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	ended := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(resp.Body) // returns only when the handler stops writing
		ended <- err
	}()

	select {
	case <-ended:
		t.Fatal("stream ended before shutdown started")
	case <-time.After(100 * time.Millisecond):
	}

	close(drain)
	select {
	case err := <-ended:
		assert.NoError(t, err, "stream should end cleanly, not with a transport error")
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not end after the drain signal")
	}
}
