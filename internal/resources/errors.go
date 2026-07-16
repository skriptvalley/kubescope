package resources

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// kubeconfigError marks a failure to load the kubeconfig or build a client for
// the active context — a local/config problem, not a cluster one. The generic
// engine maps it to a 503, matching the typed handlers' convention.
type kubeconfigError struct{ err error }

func (e *kubeconfigError) Error() string { return e.err.Error() }
func (e *kubeconfigError) Unwrap() error { return e.err }

// classifier sorts a cluster-side error into the failure taxonomy so the
// envelope can carry a precise reason code, remediation and doc link instead of
// a single opaque "cluster_unreachable" (FB-6). It is the active context's
// classifier — see classifierFor — threaded to every error writer.
type classifier = func(error) kube.Classification

// classifierFor returns the active context's error classifier for a Cluster.
func classifierFor(c Cluster) classifier { return c.ClassifyActiveError }

// writeEngineError maps an error from the generic resource engine to the
// project's structured envelope and the right HTTP status. Config/not-found and
// the 4xx write-path failures are recognized by their typed error first; every
// remaining cluster-side error is sorted through the failure taxonomy so the
// UI gets a precise reason, remediation and doc link (FB-1/FB-6):
//
//	*kubeconfigError                                   → 503 kubeconfig_unavailable
//	apierror NotFound                                  → 404 not_found
//	apierror Conflict / AlreadyExists                  → 409 conflict / already_exists
//	apierror Invalid / BadRequest                      → 422 invalid
//	Unauthorized / auth_expired                        → 401 auth_expired
//	Forbidden / forbidden                              → 403 forbidden
//	timeout                                            → 504 timeout
//	apiserver_5xx                                      → 502 apiserver_5xx
//	connection_refused / dns / tls_cert / exec plugin  → 502 <FailureClass>
//	unknown                                            → 502 cluster_unreachable
//
// A nil classifier is treated as an opaque unreachable (FailUnknown). Only
// responses of status ≥500 are logged, matching the prior default-case behavior.
func writeEngineError(w http.ResponseWriter, logger *slog.Logger, action string, err error, classify classifier) {
	message := func(e error) string { return fmt.Sprintf("%s: %v", action, e) }

	var kc *kubeconfigError
	switch {
	case errors.As(err, &kc):
		writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable", message(kc.err))
		return
	case apierrors.IsNotFound(err):
		writeError(w, logger, http.StatusNotFound, "not_found", message(err))
		return
	case apierrors.IsConflict(err):
		writeError(w, logger, http.StatusConflict, "conflict", message(err))
		return
	case apierrors.IsInvalid(err) || apierrors.IsBadRequest(err):
		writeError(w, logger, http.StatusUnprocessableEntity, "invalid", message(err))
		return
	case apierrors.IsAlreadyExists(err):
		writeError(w, logger, http.StatusConflict, "already_exists", message(err))
		return
	}

	cls := kube.Classification{Class: kube.FailUnknown}
	if classify != nil {
		cls = classify(err)
	}

	status := http.StatusBadGateway
	code := "cluster_unreachable"
	switch {
	case apierrors.IsUnauthorized(err) || cls.Class == kube.FailAuthExpired:
		status, code = http.StatusUnauthorized, "auth_expired"
	case apierrors.IsForbidden(err) || cls.Class == kube.FailForbidden:
		status, code = http.StatusForbidden, "forbidden"
	case cls.Class == kube.FailTimeout:
		status, code = http.StatusGatewayTimeout, "timeout"
	case cls.Class == kube.FailAPIServer5xx:
		status, code = http.StatusBadGateway, "apiserver_5xx"
	case cls.Class == kube.FailConnectionRefused,
		cls.Class == kube.FailDNS,
		cls.Class == kube.FailTLSCert,
		cls.Class == kube.FailExecPluginMissing:
		status, code = http.StatusBadGateway, string(cls.Class)
	default: // FailUnknown
		status, code = http.StatusBadGateway, "cluster_unreachable"
	}

	if status >= 500 {
		logger.Error(action, "error", err, "class", string(cls.Class))
	}
	writeErrorClassified(w, logger, status, code, message(err), cls.Remediation, cls.DocURL)
}

// writeMutationError maps an error from a write path (apply/scale/restart/
// delete/cordon/drain) to the structured envelope. It handles the two failure
// modes reads never see — an optimistic-concurrency conflict and server-side
// validation — before delegating everything else (NotFound/Forbidden/
// kubeconfig/cluster-unreachable) to writeEngineError. This is where a genuine
// 409/422 is surfaced faithfully instead of being collapsed to a 502 (FB-1).
// writeEngineError now recognizes the same typed cases, so the outcomes are
// identical; the early cases are kept for locality of the write-path contract.
//
//	apierror Conflict (stale resourceVersion) → 409 conflict
//	apierror Invalid / BadRequest             → 422 invalid
//	apierror AlreadyExists                     → 409 already_exists
//	anything else                              → writeEngineError
func writeMutationError(w http.ResponseWriter, logger *slog.Logger, action string, err error, classify classifier) {
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
		writeEngineError(w, logger, action, err, classify)
	}
}
