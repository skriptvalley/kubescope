package stream

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

// logLineBuffer sizes the reader→writer channel; a burst of log lines queues
// here without blocking the upstream read.
const logLineBuffer = 256

// LogCluster is the slice of the kube manager the log handler needs: a typed
// clientset for the active context to open the pod-log stream.
type LogCluster interface {
	ActiveContextName() (string, error)
	ClientsetFor(name string) (kubernetes.Interface, error)
}

// logLine is the SSE payload for one streamed log line.
type logLine struct {
	Line string `json:"line"`
}

// LogsHandler serves GET /api/v1/stream/pods/{namespace}/{name}/logs: pod logs
// over SSE with follow, container select, previous and tailLines (Story 4.3).
// Stream end (container exit, pod deletion) is surfaced as an explicit `closed`
// event, never a silent hang; client disconnect cancels the upstream read.
func LogsHandler(cluster LogCluster, logger *slog.Logger, opts ...HandlerOption) http.HandlerFunc {
	cfg := handlerConfig{heartbeat: defaultHeartbeat}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeStreamError(w, logger, http.StatusInternalServerError, "streaming_unsupported",
				"the server cannot stream responses")
			return
		}

		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")
		logOpts, err := buildPodLogOptions(r.URL.Query())
		if err != nil {
			writeStreamError(w, logger, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		ctxName, err := cluster.ActiveContextName()
		if err != nil {
			writeStreamError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}
		clientset, err := cluster.ClientsetFor(ctxName)
		if err != nil {
			writeStreamError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot build client: %v", err))
			return
		}

		body, err := clientset.CoreV1().Pods(namespace).GetLogs(name, logOpts).Stream(r.Context())
		if err != nil {
			status, code := logErrorStatus(err)
			writeStreamError(w, logger, status, code, fmt.Sprintf("opening logs for %s/%s: %v", namespace, name, err))
			return
		}
		defer func() { _ = body.Close() }()

		setSSEHeaders(w)
		flusher.Flush()
		runLogSSE(w, r, flusher, logger, body, cfg.heartbeat)
	}
}

// buildPodLogOptions maps the query into a PodLogOptions (Story 4.3 parameter
// mapping). follow defaults to true; previous logs are never followed (the
// previous container is gone), so previous forces follow off.
func buildPodLogOptions(q url.Values) (*corev1.PodLogOptions, error) {
	opts := &corev1.PodLogOptions{
		Container: q.Get("container"),
		Previous:  q.Get("previous") == "true",
		Follow:    q.Get("follow") != "false",
	}
	if opts.Previous {
		opts.Follow = false
	}
	if tl := q.Get("tailLines"); tl != "" {
		n, err := strconv.ParseInt(tl, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid tailLines %q: must be a non-negative integer", tl)
		}
		opts.TailLines = &n
	}
	return opts, nil
}

// runLogSSE pumps log lines to the client with keep-alive heartbeats, surfacing
// stream end as a `closed` event. An off-goroutine reader lets the main loop
// stay responsive to client disconnect and heartbeat ticks.
func runLogSSE(w http.ResponseWriter, r *http.Request, flusher http.Flusher, logger *slog.Logger, body io.Reader, heartbeat time.Duration) {
	ctx := r.Context()
	lines := make(chan string, logLineBuffer)
	readErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(body)
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				// Honor cancellation on the send: once the client disconnects the
				// main loop stops draining `lines`, and closing the body cannot
				// wake a goroutine already parked on a channel send — that would
				// leak the goroutine and the held upstream connection.
				select {
				case lines <- strings.TrimRight(line, "\n"):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err // buffered(1), single send — never blocks
				close(lines)
				return
			}
		}
	}()

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := w.Write(heartbeatComment); err != nil {
				return
			}
			flusher.Flush()
		case line, ok := <-lines:
			if !ok {
				writeLogClosed(w, flusher, logger, <-readErr)
				return
			}
			data, err := json.Marshal(logLine{Line: line})
			if err != nil {
				logger.Error("marshaling log line", "error", err)
				continue
			}
			if !writeSSEFrame(w, flusher, "", data) {
				return
			}
		}
	}
}

// writeLogClosed emits the terminal `closed` event. EOF/nil is a clean end; any
// other error is reported as the close reason so the UI shows why it stopped.
func writeLogClosed(w http.ResponseWriter, flusher http.Flusher, logger *slog.Logger, err error) {
	reason := "eof"
	if err != nil && !errors.Is(err, io.EOF) {
		reason = err.Error()
	}
	data, mErr := json.Marshal(map[string]string{"reason": reason})
	if mErr != nil {
		logger.Error("marshaling log close", "error", mErr)
		return
	}
	writeSSEFrame(w, flusher, "closed", data)
}

// writeSSEFrame writes one SSE frame (optionally named) and flushes, returning
// false on a write error so the caller stops.
func writeSSEFrame(w http.ResponseWriter, flusher http.Flusher, event string, data []byte) bool {
	var b strings.Builder
	if event != "" {
		fmt.Fprintf(&b, "event: %s\n", event)
	}
	fmt.Fprintf(&b, "data: %s\n\n", data)
	if _, err := io.WriteString(w, b.String()); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// logErrorStatus maps a GetLogs failure to an HTTP status + code, mirroring the
// REST engine's taxonomy (missing pod → 404, RBAC → 403, else upstream 502).
func logErrorStatus(err error) (int, string) {
	switch {
	case apierrors.IsNotFound(err):
		return http.StatusNotFound, "not_found"
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		return http.StatusForbidden, "forbidden"
	default:
		return http.StatusBadGateway, "cluster_unreachable"
	}
}
