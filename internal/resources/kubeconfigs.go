package resources

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// maxKubeconfigSourceBodyBytes caps the add-source request body; the payload is
// a single small {"path":"..."} object.
const maxKubeconfigSourceBodyBytes = 64 << 10

// guidanceMountedDir is the remediation for an invisible runtime-added path: in
// Docker only paths under a mount made at container creation are visible, so the
// workflow is to mount a directory once and drop kubeconfig files into it
// (ADR-0008). Runtime mounting does not exist; this is the only safe Docker add.
const guidanceMountedDir = "In Docker only paths under mounts made at container creation are visible — " +
	"mount a directory once (-v ~/.kube:/kubeconfigs:ro) and drop kubeconfig files into it. The registry is unchanged."

// guidanceInvalidSource is the remediation for a present-but-unusable path.
const guidanceInvalidSource = "The path must be a readable kubeconfig file with at least one context, or a " +
	"directory of kubeconfig files. The registry is unchanged."

type addSourceRequest struct {
	Path string `json:"path"`
}

// kubeconfigSourceList is the GET /api/v1/kubeconfigs response and the body both
// mutation endpoints echo on success: the expanded source registry plus whether
// the runtime controls are available.
type kubeconfigSourceList struct {
	Sources          []kube.SourceStatus `json:"sources"`
	CanSetKubeconfig bool                `json:"canSetKubeconfig"`
}

// ListKubeconfigSourcesHandler serves GET /api/v1/kubeconfigs: the expanded
// source registry (per-source status, per-file expansion for directories,
// contributed/shadowed context names) in precedence order. It is registered
// unguarded (like /setup) and always answers 200 — the registry is
// cluster-independent and re-scanned per request, so "Rescan" is a plain
// refetch. canSetKubeconfig folds in read-only mode server-side.
func ListKubeconfigSourcesHandler(c Cluster, canSetKubeconfig bool, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, logger, http.StatusOK, kubeconfigSourceList{
			Sources:          c.Sources(),
			CanSetKubeconfig: canSetKubeconfig,
		})
	}
}

// AddKubeconfigSourceHandler serves POST /api/v1/kubeconfigs: register a runtime
// kubeconfig source (ADR-0008). Registered inside the read-only guarded group,
// so read-only mode 403s it first; the AllowKubeconfigSet flag gates it further.
// A malformed/relative/empty path is 400; a duplicate is 409; an invisible or
// invalid path is 422 (the registry left untouched) — an invisible path carries
// the mounted-directory guidance. Success returns the fresh listing.
func AddKubeconfigSourceHandler(c Cluster, allowKubeconfigSet bool, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowKubeconfigSet {
			writeErrorGuidance(w, logger, http.StatusForbidden, "kubeconfig_set_disabled",
				"registering a kubeconfig source at runtime is disabled",
				"Set KUBESCOPE_ALLOW_KUBECONFIG_SET=true to enable managing kubeconfig sources at runtime (ADR-0008).")
			return
		}

		var req addSourceRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxKubeconfigSourceBodyBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, logger, http.StatusBadRequest, "invalid_request",
				`request body must be JSON with a "path" field`)
			return
		}
		if dec.More() {
			writeError(w, logger, http.StatusBadRequest, "invalid_request",
				"request body must contain a single JSON object")
			return
		}
		if req.Path == "" || !filepath.IsAbs(req.Path) {
			writeError(w, logger, http.StatusBadRequest, "invalid_request",
				"path must be a non-empty absolute path")
			return
		}

		if err := c.AddSource(req.Path); err != nil {
			var dup *kube.DuplicateSourceError
			if errors.As(err, &dup) {
				writeError(w, logger, http.StatusConflict, "kubeconfig_source_exists", err.Error())
				return
			}
			var invisible *kube.SourceInvisibleError
			if errors.As(err, &invisible) {
				// An invisible path names the mounted-directory workflow + ADR-0004.
				writeErrorClassified(w, logger, http.StatusUnprocessableEntity, "kubeconfig_invalid",
					err.Error(), guidanceMountedDir, docKubeconfigInDocker)
				return
			}
			writeErrorGuidance(w, logger, http.StatusUnprocessableEntity, "kubeconfig_invalid",
				err.Error(), guidanceInvalidSource)
			return
		}
		// The path is a mount point / operator-supplied location, not file
		// contents — safe to log; the kubeconfig itself is never logged.
		logger.Info("kubeconfig source added", "path", req.Path)
		writeJSON(w, logger, http.StatusOK, kubeconfigSourceList{
			Sources:          c.Sources(),
			CanSetKubeconfig: allowKubeconfigSet,
		})
	}
}

// RemoveKubeconfigSourceHandler serves DELETE /api/v1/kubeconfigs/{id}: drop a
// registered source by its id (ADR-0008). Registered inside the read-only
// guarded group and flag-gated like the add endpoint. An unknown id is 404; any
// registered source (env baseline or runtime) is removable and the registry may
// become empty. Success returns the fresh listing.
func RemoveKubeconfigSourceHandler(c Cluster, allowKubeconfigSet bool, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowKubeconfigSet {
			writeErrorGuidance(w, logger, http.StatusForbidden, "kubeconfig_set_disabled",
				"removing a kubeconfig source at runtime is disabled",
				"Set KUBESCOPE_ALLOW_KUBECONFIG_SET=true to enable managing kubeconfig sources at runtime (ADR-0008).")
			return
		}

		id := chi.URLParam(r, "id")
		if err := c.RemoveSource(id); err != nil {
			var unknown *kube.UnknownSourceError
			if errors.As(err, &unknown) {
				writeError(w, logger, http.StatusNotFound, "kubeconfig_source_not_found", err.Error())
				return
			}
			writeError(w, logger, http.StatusUnprocessableEntity, "kubeconfig_invalid", err.Error())
			return
		}
		logger.Info("kubeconfig source removed", "id", id)
		writeJSON(w, logger, http.StatusOK, kubeconfigSourceList{
			Sources:          c.Sources(),
			CanSetKubeconfig: allowKubeconfigSet,
		})
	}
}
