package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// defaultExecCommand is the shell run when the client requests no explicit
// command — the interactive-terminal default.
const defaultExecCommand = "/bin/sh"

// execReadLimit caps a single inbound WebSocket message. Keystrokes are tiny;
// the headroom is for clipboard pastes, which arrive as one message.
const execReadLimit = 1 << 20 // 1 MiB

// execCloseTimeout bounds the final control frame + close handshake so a dead
// peer can never park the handler goroutine on the teardown write.
const execCloseTimeout = 5 * time.Second

// wsCloseReasonMax is the WebSocket close-frame reason limit (RFC 6455: the
// control-frame payload is ≤125 bytes, of which 2 are the status code).
const wsCloseReasonMax = 123

// Exec wire protocol (ADR-0006). The exec terminal is genuinely bidirectional,
// so it rides a WebSocket while watches/logs stay on SSE. Two frame kinds:
//
//   - Binary frames carry raw terminal bytes: client→server is stdin
//     (keystrokes, pastes); server→client is stdout/stderr merged (TTY mode
//     collapses them into one stream).
//   - Text frames carry a JSON control message (controlMessage). Client→server
//     sends "resize"; server→client sends a terminal "exit" (with the process
//     exit code) or "error" (a structured failure reason) immediately before it
//     closes the socket. The close frame carries the same intent as its status
//     code, but the control frame is the authoritative, untruncated payload.
const (
	controlResize = "resize" // client→server: terminal size changed
	controlExit   = "exit"   // server→client: remote process exited (Code set)
	controlError  = "error"  // server→client: session failed (Message set)
)

// controlMessage is a text-frame control payload on the exec WebSocket. Fields
// are populated per Type; unused ones are omitted.
type controlMessage struct {
	Type    string `json:"type"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// parseControl decodes a text control frame, rejecting one with no type so a
// garbled frame never masquerades as a resize.
func parseControl(data []byte) (controlMessage, error) {
	var m controlMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return controlMessage{}, err
	}
	if m.Type == "" {
		return controlMessage{}, errors.New("control message missing type")
	}
	return m, nil
}

// execOptions is the parsed exec target: which container, and the command to run.
type execOptions struct {
	container string
	command   []string
}

// buildExecOptions maps the query into an exec target. `container` selects the
// container (empty lets the apiserver pick the default). `command` is repeatable
// (e.g. ?command=/bin/sh) and defaults to the shell; empty values are dropped so
// a stray `&command=` never injects an empty arg.
func buildExecOptions(q url.Values) execOptions {
	o := execOptions{container: q.Get("container")}
	for _, c := range q["command"] {
		if c != "" {
			o.command = append(o.command, c)
		}
	}
	if len(o.command) == 0 {
		o.command = []string{defaultExecCommand}
	}
	return o
}

// ExecCluster is the slice of the kube manager the exec handler needs: resolve
// the active context, and build the typed clientset (for the exec request URL)
// and rest.Config (for the SPDY executor) under that same context name so they
// cannot diverge under a concurrent switch. *kube.Manager satisfies it.
type ExecCluster interface {
	ActiveContextName() (string, error)
	ClientsetFor(name string) (kubernetes.Interface, error)
	RestConfigFor(name string) (*rest.Config, error)
}

// executorFactory builds the remote executor for a resolved exec request. The
// default wires client-go's SPDY executor; tests inject a fake so the WebSocket
// bridge is exercised without a live apiserver.
type executorFactory func(cfg *rest.Config, u *url.URL) (remotecommand.Executor, error)

func defaultExecutorFactory(cfg *rest.Config, u *url.URL) (remotecommand.Executor, error) {
	return remotecommand.NewSPDYExecutor(cfg, http.MethodPost, u)
}

type execConfig struct {
	factory executorFactory
}

// ExecOption tunes the exec handler.
type ExecOption func(*execConfig)

// withExecutorFactory overrides how the remote executor is built (tests only).
func withExecutorFactory(f executorFactory) ExecOption {
	return func(c *execConfig) { c.factory = f }
}

// ExecHandler serves GET /api/v1/stream/pods/{namespace}/{name}/exec: it
// upgrades to a WebSocket and bridges it to a client-go SPDY exec session
// against the target pod/container (Story 6.1, ADR-0006). Read-only mode is
// enforced by middleware ahead of this handler, so the 403 lands before the
// upgrade. Once upgraded, every failure — kubeconfig unavailable, pod gone, bad
// container, RBAC denied, remote process exit — is surfaced as a structured
// control frame plus a close, never a silent hang.
func ExecHandler(cluster ExecCluster, reg *ExecRegistry, logger *slog.Logger, opts ...ExecOption) http.HandlerFunc {
	cfg := execConfig{factory: defaultExecutorFactory}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")
		target := buildExecOptions(r.URL.Query())

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// Same-origin is always authorized (the embedded SPA); these patterns
			// additionally allow the Vite dev proxy (make dev) without opening the
			// exec socket to arbitrary cross-origin pages. Patterns are matched with
			// path.Match, so the IPv6-loopback brackets must be escaped or they are
			// read as a character class (a dead, never-matching entry).
			OriginPatterns: []string{"localhost:*", "127.0.0.1:*", `\[::1\]:*`},
		})
		if err != nil {
			logger.Warn("exec websocket upgrade failed", "error", err)
			return
		}
		conn.SetReadLimit(execReadLimit)
		defer func() { _ = conn.CloseNow() }()

		// Resolve the cluster under one context name; surface any failure as a
		// structured error frame over the now-open socket.
		ctxName, err := cluster.ActiveContextName()
		if err != nil {
			closeExecError(conn, fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}
		clientset, err := cluster.ClientsetFor(ctxName)
		if err != nil {
			closeExecError(conn, fmt.Sprintf("cannot build client: %v", err))
			return
		}
		restCfg, err := cluster.RestConfigFor(ctxName)
		if err != nil {
			closeExecError(conn, fmt.Sprintf("cannot build client config: %v", err))
			return
		}

		req := clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Namespace(namespace).
			Name(name).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: target.container,
				Command:   target.command,
				Stdin:     true,
				Stdout:    true,
				Stderr:    false, // TTY merges stderr into stdout
				TTY:       true,
			}, scheme.ParameterCodec)

		exec, err := cfg.factory(restCfg, req.URL())
		if err != nil {
			closeExecError(conn, fmt.Sprintf("creating exec session: %v", err))
			return
		}

		// Bind the session to its context so a context switch or server shutdown
		// tears it down, and to the request context so a client disconnect does.
		session := reg.add(ctxName, r.Context())
		defer session.close()

		runExecSession(session.ctx, conn, exec, logger)
	}
}

// runExecSession pumps the WebSocket ⇆ SPDY exec bridge until either side ends:
// binary frames become stdin, text frames drive terminal resizes, and the
// remote stdout streams back as binary frames. The read loop cancels the shared
// context on client disconnect so the executor is torn down (no leaked
// goroutine); when the executor returns, the outcome is reported and the socket
// closed.
func runExecSession(ctx context.Context, conn *websocket.Conn, exec remotecommand.Executor, logger *slog.Logger) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stdinR, stdinW := io.Pipe()
	sizes := newTermSizeQueue(ctx)

	go func() {
		// Client gone (or a read error): EOF the executor's stdin and cancel the
		// session so StreamWithContext returns instead of hanging.
		defer cancel()
		defer func() { _ = stdinW.Close() }()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			switch typ {
			case websocket.MessageText:
				if msg, err := parseControl(data); err == nil && msg.Type == controlResize {
					sizes.push(msg.Cols, msg.Rows)
				}
				// Unknown/garbled control frames are ignored — never fatal.
			case websocket.MessageBinary:
				if _, err := stdinW.Write(data); err != nil {
					return
				}
			}
		}
	}()

	err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdinR,
		Stdout:            &wsWriter{ctx: ctx, conn: conn},
		Tty:               true,
		TerminalSizeQueue: sizes,
	})
	// Report the outcome and close the socket BEFORE tearing the context down.
	// Cancelling the read context makes coder/websocket close the underlying
	// connection (Read arms a cancel→close), which would race — and usually drop
	// — the authoritative exit/error control frame and the close. closeExecResult
	// does its own Close, which unblocks a read loop parked in conn.Read.
	closeExecResult(conn, logger, err)
	cancel()
	// Unblock a read loop parked in stdinW.Write: once the executor has returned
	// nothing drains the pipe, so a stray stdin frame in flight would hang the
	// goroutine (conn.Close does not wake a pipe write). Closing the read end
	// makes that Write return promptly.
	_ = stdinR.Close()
}

// wsWriter adapts a WebSocket connection into an io.Writer of binary frames —
// the executor's stdout sink. Each Write is one frame.
type wsWriter struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (int, error) {
	if err := w.conn.Write(w.ctx, websocket.MessageBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// closeExecResult reports the executor's outcome and closes the socket. A clean
// or non-zero process exit becomes an `exit` frame + normal close; a context
// cancellation (client disconnect / context switch / shutdown) becomes a plain
// going-away close; any other failure becomes an `error` frame + internal-error
// close so the UI shows why the session ended.
func closeExecResult(conn *websocket.Conn, logger *slog.Logger, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), execCloseTimeout)
	defer cancel()

	switch {
	case err == nil:
		writeControl(ctx, conn, controlMessage{Type: controlExit, Code: 0})
		_ = conn.Close(websocket.StatusNormalClosure, "process exited")
	case errors.Is(err, context.Canceled):
		// The session was torn down (client gone, context switch, shutdown); the
		// socket is likely already dead, so just attempt a graceful close.
		_ = conn.Close(websocket.StatusGoingAway, "session ended")
	default:
		var codeErr utilexec.CodeExitError
		if errors.As(err, &codeErr) {
			writeControl(ctx, conn, controlMessage{Type: controlExit, Code: codeErr.Code})
			_ = conn.Close(websocket.StatusNormalClosure, "process exited")
			return
		}
		logger.Warn("exec session ended with error", "error", err)
		msg := err.Error()
		writeControl(ctx, conn, controlMessage{Type: controlError, Message: msg})
		_ = conn.Close(websocket.StatusInternalError, truncateReason(msg))
	}
}

// closeExecError reports a pre-stream failure (kubeconfig/client resolution)
// over the already-open socket as an `error` frame + close.
func closeExecError(conn *websocket.Conn, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), execCloseTimeout)
	defer cancel()
	writeControl(ctx, conn, controlMessage{Type: controlError, Message: message})
	_ = conn.Close(websocket.StatusInternalError, truncateReason(message))
}

// writeControl marshals and sends one text control frame, best-effort — a dead
// peer just means the close that follows also fails, which is fine.
func writeControl(ctx context.Context, conn *websocket.Conn, msg controlMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, data)
}

// truncateReason clamps a close-frame reason to the RFC 6455 limit.
func truncateReason(s string) string {
	if len(s) > wsCloseReasonMax {
		return s[:wsCloseReasonMax]
	}
	return s
}

// termSizeQueue adapts resize control frames into a remotecommand.TerminalSizeQueue.
// It keeps only the latest size (a size-1 buffer): intermediate sizes during a
// fast drag are irrelevant once a newer one arrives. Next() blocks until a size
// is available or the session context ends, at which point it returns nil to
// stop the executor's resize monitor.
type termSizeQueue struct {
	ctx context.Context
	ch  chan remotecommand.TerminalSize
}

func newTermSizeQueue(ctx context.Context) *termSizeQueue {
	return &termSizeQueue{ctx: ctx, ch: make(chan remotecommand.TerminalSize, 1)}
}

func (q *termSizeQueue) push(cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return // a zero dimension is not a real terminal size
	}
	size := remotecommand.TerminalSize{Width: cols, Height: rows}
	for {
		select {
		case q.ch <- size:
			return
		default:
			// Buffer full: drop the stale pending size and retry with the newest.
			select {
			case <-q.ch:
			default:
			}
		}
	}
}

func (q *termSizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case size := <-q.ch:
		return &size
	case <-q.ctx.Done():
		return nil
	}
}

// ExecRegistry tracks live exec sessions so they can be torn down as a group on
// a context switch (sessions bound to another context) or on server shutdown
// (all of them). Each session also self-terminates on client disconnect via its
// request-scoped context; the registry only adds the group-teardown levers.
type ExecRegistry struct {
	mu       sync.Mutex
	sessions map[*execSession]struct{}
}

// NewExecRegistry returns an empty registry.
func NewExecRegistry() *ExecRegistry {
	return &ExecRegistry{sessions: make(map[*execSession]struct{})}
}

// execSession is one tracked exec session: a cancelable context derived from the
// request, tagged with the context it is bound to.
type execSession struct {
	reg     *ExecRegistry
	context string
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
}

// add registers a new session bound to contextName, derived from parent so a
// client disconnect (parent cancellation) still tears it down.
func (reg *ExecRegistry) add(contextName string, parent context.Context) *execSession {
	ctx, cancel := context.WithCancel(parent)
	s := &execSession{reg: reg, context: contextName, ctx: ctx, cancel: cancel}
	reg.mu.Lock()
	reg.sessions[s] = struct{}{}
	reg.mu.Unlock()
	return s
}

// close cancels the session and removes it from the registry, exactly once.
func (s *execSession) close() {
	s.once.Do(func() {
		s.cancel()
		s.reg.mu.Lock()
		delete(s.reg.sessions, s)
		s.reg.mu.Unlock()
	})
}

// CloseOthers cancels every session not bound to the current context — the
// context-switch teardown. Handlers observe the cancellation and deregister
// themselves.
func (reg *ExecRegistry) CloseOthers(current string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for s := range reg.sessions {
		if s.context != current {
			s.cancel()
		}
	}
}

// CloseAll cancels every session — the shutdown teardown.
func (reg *ExecRegistry) CloseAll() {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	for s := range reg.sessions {
		s.cancel()
	}
}

// active reports the number of live sessions — teardown assertions in tests.
func (reg *ExecRegistry) active() int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return len(reg.sessions)
}
