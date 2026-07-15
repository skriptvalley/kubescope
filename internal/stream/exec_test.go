package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// execTestSentinel is a byte the fake executor treats as "process exit": on
// reading it the executor stops and returns its configured exit result. It lets
// a test drive a clean session end without closing the socket first.
const execTestSentinel = 0x04

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeExecutor stands in for a client-go SPDY executor: it echoes stdin back to
// stdout (so the WebSocket bridge is exercised end-to-end), forwards terminal
// resizes to onResize, and returns exitErr when it sees the sentinel or stdin EOF.
type fakeExecutor struct {
	exitErr  error
	onResize func(remotecommand.TerminalSize)
}

func (f *fakeExecutor) Stream(o remotecommand.StreamOptions) error {
	return f.StreamWithContext(context.Background(), o)
}

func (f *fakeExecutor) StreamWithContext(_ context.Context, o remotecommand.StreamOptions) error {
	if o.TerminalSizeQueue != nil && f.onResize != nil {
		go func() {
			for {
				size := o.TerminalSizeQueue.Next()
				if size == nil {
					return
				}
				f.onResize(*size)
			}
		}()
	}
	buf := make([]byte, 4096)
	for {
		n, err := o.Stdin.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if i := bytes.IndexByte(chunk, execTestSentinel); i >= 0 {
				if i > 0 {
					_, _ = o.Stdout.Write(chunk[:i])
				}
				return f.exitErr
			}
			if _, werr := o.Stdout.Write(chunk); werr != nil {
				return werr
			}
		}
		if err != nil {
			return f.exitErr
		}
	}
}

// serveExecSession runs one runExecSession over a real WebSocket and returns the
// dialed client end.
func serveExecSession(t *testing.T, exec remotecommand.Executor) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		runExecSession(r.Context(), conn, exec, discardLogger())
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestBuildExecOptions(t *testing.T) {
	t.Run("defaults to the shell", func(t *testing.T) {
		o := buildExecOptions(url.Values{})
		assert.Equal(t, []string{defaultExecCommand}, o.command)
		assert.Equal(t, "", o.container)
	})
	t.Run("passes container and repeated command through in order", func(t *testing.T) {
		o := buildExecOptions(url.Values{"container": {"web"}, "command": {"/bin/bash", "-lc", "top"}})
		assert.Equal(t, "web", o.container)
		assert.Equal(t, []string{"/bin/bash", "-lc", "top"}, o.command)
	})
	t.Run("drops empty command values, falling back to the shell", func(t *testing.T) {
		o := buildExecOptions(url.Values{"command": {"", ""}})
		assert.Equal(t, []string{defaultExecCommand}, o.command)
	})
}

func TestParseControl(t *testing.T) {
	t.Run("decodes a resize frame", func(t *testing.T) {
		msg, err := parseControl([]byte(`{"type":"resize","cols":120,"rows":40}`))
		require.NoError(t, err)
		assert.Equal(t, controlResize, msg.Type)
		assert.Equal(t, uint16(120), msg.Cols)
		assert.Equal(t, uint16(40), msg.Rows)
	})
	t.Run("rejects a frame with no type", func(t *testing.T) {
		_, err := parseControl([]byte(`{"cols":80}`))
		assert.Error(t, err)
	})
	t.Run("rejects non-JSON", func(t *testing.T) {
		_, err := parseControl([]byte("not json"))
		assert.Error(t, err)
	})
}

func TestTermSizeQueue(t *testing.T) {
	t.Run("returns the latest pushed size", func(t *testing.T) {
		q := newTermSizeQueue(context.Background())
		q.push(80, 24)
		q.push(120, 40) // supersedes the queued 80x24
		size := q.Next()
		require.NotNil(t, size)
		assert.Equal(t, remotecommand.TerminalSize{Width: 120, Height: 40}, *size)
	})
	t.Run("ignores zero dimensions", func(t *testing.T) {
		q := newTermSizeQueue(context.Background())
		q.push(0, 24)
		q.push(80, 0)
		done := make(chan *remotecommand.TerminalSize, 1)
		go func() { done <- q.Next() }()
		select {
		case <-done:
			t.Fatal("Next returned for a zero-dimension push")
		case <-time.After(100 * time.Millisecond):
		}
	})
	t.Run("returns nil when the context ends", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		q := newTermSizeQueue(ctx)
		cancel()
		assert.Nil(t, q.Next())
	})
}

func TestRunExecSessionEchoesAndExitsCleanly(t *testing.T) {
	c := serveExecSession(t, &fakeExecutor{exitErr: nil})
	ctx := context.Background()

	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte("hello")))
	typ, data, err := c.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageBinary, typ)
	assert.Equal(t, "hello", string(data))

	// The sentinel triggers a clean process exit → structured exit frame + close.
	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte{execTestSentinel}))
	typ, data, err = c.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, typ)
	var msg controlMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	assert.Equal(t, controlExit, msg.Type)
	assert.Equal(t, 0, msg.Code)

	_, _, err = c.Read(ctx)
	assert.Equal(t, websocket.StatusNormalClosure, websocket.CloseStatus(err))
}

func TestRunExecSessionForwardsResize(t *testing.T) {
	resized := make(chan remotecommand.TerminalSize, 1)
	c := serveExecSession(t, &fakeExecutor{onResize: func(s remotecommand.TerminalSize) { resized <- s }})
	ctx := context.Background()

	require.NoError(t, c.Write(ctx, websocket.MessageText,
		mustJSON(t, controlMessage{Type: controlResize, Cols: 120, Rows: 40})))

	select {
	case s := <-resized:
		assert.Equal(t, remotecommand.TerminalSize{Width: 120, Height: 40}, s)
	case <-time.After(2 * time.Second):
		t.Fatal("resize control frame never reached the executor")
	}
}

func TestRunExecSessionSurfacesNonZeroExit(t *testing.T) {
	c := serveExecSession(t, &fakeExecutor{
		exitErr: utilexec.CodeExitError{Err: errors.New("command terminated with exit code 7"), Code: 7},
	})
	ctx := context.Background()

	require.NoError(t, c.Write(ctx, websocket.MessageBinary, []byte{execTestSentinel}))
	typ, data, err := c.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, typ)
	var msg controlMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	assert.Equal(t, controlExit, msg.Type)
	assert.Equal(t, 7, msg.Code)

	_, _, err = c.Read(ctx)
	assert.Equal(t, websocket.StatusNormalClosure, websocket.CloseStatus(err))
}

// fakeExecCluster resolves to a fixed context; RestConfigFor can be made to fail
// to exercise the post-upgrade structured-error path.
type fakeExecCluster struct {
	cs      kubernetes.Interface
	restErr error
}

func (f *fakeExecCluster) ActiveContextName() (string, error) { return "ctx", nil }
func (f *fakeExecCluster) ClientsetFor(string) (kubernetes.Interface, error) {
	return f.cs, nil
}
func (f *fakeExecCluster) RestConfigFor(string) (*rest.Config, error) {
	if f.restErr != nil {
		return nil, f.restErr
	}
	return &rest.Config{Host: "https://example.test"}, nil
}

func TestExecHandlerReportsResolutionErrorOverSocket(t *testing.T) {
	cluster := &fakeExecCluster{cs: fake.NewClientset(), restErr: errors.New("kubeconfig boom")}
	r := chi.NewRouter()
	r.Get("/exec/pods/{namespace}/{name}/exec", ExecHandler(cluster, NewExecRegistry(), discardLogger()))
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/exec/pods/default/web/exec", nil)
	require.NoError(t, err)
	defer func() { _ = c.CloseNow() }()

	typ, data, err := c.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, typ)
	var msg controlMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	assert.Equal(t, controlError, msg.Type)
	assert.Contains(t, msg.Message, "kubeconfig boom")

	_, _, err = c.Read(ctx)
	assert.Equal(t, websocket.StatusInternalError, websocket.CloseStatus(err))
}

func TestExecRegistryTeardown(t *testing.T) {
	reg := NewExecRegistry()
	a := reg.add("ctx-a", context.Background())
	b := reg.add("ctx-b", context.Background())
	require.Equal(t, 2, reg.active())

	// A context switch to ctx-a cancels only the ctx-b session.
	reg.CloseOthers("ctx-a")
	assert.NoError(t, a.ctx.Err())
	assert.Error(t, b.ctx.Err())

	// Shutdown cancels everything.
	reg.CloseAll()
	assert.Error(t, a.ctx.Err())

	// Handlers deregister via close(); it is idempotent.
	a.close()
	b.close()
	a.close()
	assert.Equal(t, 0, reg.active())
}
