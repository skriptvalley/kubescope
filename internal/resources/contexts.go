package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// Cluster is the context/cluster surface the context and overview handlers
// need. Implemented by *kube.Manager; faked in tests. It is a superset of
// ClientsetProvider, so the same value satisfies the node handler too.
type Cluster interface {
	Clientset() (kubernetes.Interface, error)
	ActiveContextName() (string, error)
	Contexts() ([]kube.ContextInfo, error)
	SwitchContext(name string) error
	ProbeAll(ctx context.Context) ([]kube.ContextHealth, error)
	// ExecGuidance returns ADR-0004 exec-plugin guidance for a context that
	// uses an exec credential plugin, or "" otherwise.
	ExecGuidance(name string) string
	// Dynamic and DiscoveryFor back the generic resource engine (ADR-0003):
	// enumerate every GVR incl. CRDs, then get/list any of them. DiscoveryFor
	// takes an explicit context name (see DiscoveryCluster) so the cache key
	// and the fetched client cannot diverge under a concurrent switch.
	Dynamic() (dynamic.Interface, error)
	DiscoveryFor(name string) (discovery.DiscoveryInterface, error)
}

// maxSwitchBodyBytes caps the context-switch request body; the payload is a
// single small {"name":"..."} object.
const maxSwitchBodyBytes = 64 << 10

type contextList struct {
	Items []kube.ContextInfo `json:"items"`
}

type healthList struct {
	Items []kube.ContextHealth `json:"items"`
}

type switchRequest struct {
	Name string `json:"name"`
}

// ContextsHandler serves GET /api/v1/contexts: every context with its cluster,
// default namespace and which one is active. A missing/malformed kubeconfig is
// a structured 503 — the server stays up.
func ContextsHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		infos, err := cluster.Contexts()
		if err != nil {
			logger.Error("enumerating contexts", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot read kubeconfig: %v", err))
			return
		}
		writeJSON(w, logger, http.StatusOK, contextList{Items: infos})
	}
}

// SwitchContextHandler serves POST /api/v1/contexts/switch: sets the active
// context (in memory only) so subsequent API calls target the new cluster.
// Returns the refreshed context list. Unknown contexts are a 404.
func SwitchContextHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req switchRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSwitchBodyBytes))
		if err := dec.Decode(&req); err != nil {
			writeError(w, logger, http.StatusBadRequest, "invalid_request",
				`request body must be JSON with a "name" field`)
			return
		}
		// Reject trailing data after the first JSON value (e.g. two objects).
		if dec.More() {
			writeError(w, logger, http.StatusBadRequest, "invalid_request",
				"request body must contain a single JSON object")
			return
		}
		if req.Name == "" {
			writeError(w, logger, http.StatusBadRequest, "invalid_request", "context name must not be empty")
			return
		}
		if err := cluster.SwitchContext(req.Name); err != nil {
			var unknown *kube.UnknownContextError
			if errors.As(err, &unknown) {
				writeError(w, logger, http.StatusNotFound, "unknown_context", err.Error())
				return
			}
			logger.Error("switching context", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot switch context: %v", err))
			return
		}
		infos, err := cluster.Contexts()
		if err != nil {
			logger.Error("re-reading contexts after switch", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("switched, but cannot re-read kubeconfig: %v", err))
			return
		}
		writeJSON(w, logger, http.StatusOK, contextList{Items: infos})
	}
}

// HealthHandler serves GET /api/v1/contexts/health: a concurrent reachability /
// auth / server-version probe per context. Exec-plugin failures carry ADR-0004
// guidance instead of a raw error.
func HealthHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health, err := cluster.ProbeAll(r.Context())
		if err != nil {
			logger.Error("probing contexts", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot read kubeconfig: %v", err))
			return
		}
		writeJSON(w, logger, http.StatusOK, healthList{Items: health})
	}
}
