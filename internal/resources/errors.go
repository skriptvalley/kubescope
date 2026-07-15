package resources

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// kubeconfigError marks a failure to load the kubeconfig or build a client for
// the active context — a local/config problem, not a cluster one. The generic
// engine maps it to a 503, matching the typed handlers' convention.
type kubeconfigError struct{ err error }

func (e *kubeconfigError) Error() string { return e.err.Error() }
func (e *kubeconfigError) Unwrap() error { return e.err }

// writeEngineError maps an error from the generic resource engine to the
// project's structured envelope and the right HTTP status:
//
//	*kubeconfigError            → 503 kubeconfig_unavailable
//	apierror NotFound           → 404 not_found
//	apierror Forbidden/Unauth   → 403 forbidden
//	anything else (cluster-side)→ 502 cluster_unreachable (+ optional guidance)
//
// guidance is ADR-0004 exec-plugin remediation attached to the 502 case only.
func writeEngineError(w http.ResponseWriter, logger *slog.Logger, action string, err error, guidance string) {
	var kc *kubeconfigError
	switch {
	case errors.As(err, &kc):
		writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
			fmt.Sprintf("%s: %v", action, kc.err))
	case apierrors.IsNotFound(err):
		writeError(w, logger, http.StatusNotFound, "not_found",
			fmt.Sprintf("%s: %v", action, err))
	case apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err):
		writeError(w, logger, http.StatusForbidden, "forbidden",
			fmt.Sprintf("%s: %v", action, err))
	default:
		logger.Error(action, "error", err)
		writeErrorGuidance(w, logger, http.StatusBadGateway, "cluster_unreachable",
			fmt.Sprintf("%s: %v", action, err), guidance)
	}
}

// writeMutationError maps an error from a write path (apply/scale/restart/
// delete/cordon/drain) to the structured envelope. It handles the two failure
// modes reads never see — an optimistic-concurrency conflict and server-side
// validation — before delegating everything else (NotFound/Forbidden/
// kubeconfig/cluster-unreachable) to writeEngineError. This is where a genuine
// 409/422 is surfaced faithfully instead of being collapsed to a 502 (FB-1).
//
//	apierror Conflict (stale resourceVersion) → 409 conflict
//	apierror Invalid / BadRequest             → 422 invalid
//	apierror AlreadyExists                     → 409 already_exists
//	anything else                              → writeEngineError (404/403/503/502)
func writeMutationError(w http.ResponseWriter, logger *slog.Logger, action string, err error, guidance string) {
	switch {
	case apierrors.IsConflict(err):
		writeError(w, logger, http.StatusConflict, "conflict",
			fmt.Sprintf("%s: %v", action, err))
	case apierrors.IsInvalid(err) || apierrors.IsBadRequest(err):
		writeError(w, logger, http.StatusUnprocessableEntity, "invalid",
			fmt.Sprintf("%s: %v", action, err))
	case apierrors.IsAlreadyExists(err):
		writeError(w, logger, http.StatusConflict, "already_exists",
			fmt.Sprintf("%s: %v", action, err))
	default:
		writeEngineError(w, logger, action, err, guidance)
	}
}

// execGuidanceFor resolves the active context's ADR-0004 exec-plugin guidance,
// or "" when there is no active context or it uses no exec plugin.
func execGuidanceFor(c Cluster) string {
	name, err := c.ActiveContextName()
	if err != nil {
		return ""
	}
	return c.ExecGuidance(name)
}
