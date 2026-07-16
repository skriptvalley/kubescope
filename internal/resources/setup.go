package resources

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

// docKubeconfigInDocker is the auth/kubeconfig-in-Docker doc surfaced by the
// no-kubeconfig setup state; the same link classification.DocURL carries for
// connectivity failures (ADR-0004).
const docKubeconfigInDocker = "https://github.com/skriptvalley/kubescope/blob/main/docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md"

// maxSetKubeconfigBodyBytes caps the set-kubeconfig request body; the payload is
// a single small {"path":"..."} object.
const maxSetKubeconfigBodyBytes = 64 << 10

// SetupState is the first-run / connectivity posture the frontend reads to
// decide between the app and a guided starter screen (FB-6, ADR-0007). It is
// cluster-state-derived but never fails: every branch resolves to a 200 with a
// stable state, a machine-readable reason and human remediation.
type SetupState struct {
	// State is the coarse posture: no_kubeconfig | no_contexts |
	// no_active_context | active_unreachable | ready.
	State string `json:"state"`
	// Reason is the machine-readable cause: kubeconfig_missing |
	// kubeconfig_invalid | no_current_context | <FailureClass> | "".
	Reason string `json:"reason,omitempty"`
	// Message is a human summary that may embed the underlying error.
	Message string `json:"message,omitempty"`
	// Guidance is actionable remediation for this state.
	Guidance string `json:"guidance,omitempty"`
	// DocURL links to the doc covering this state's fix, when one applies.
	DocURL string `json:"docURL,omitempty"`
	// KubeconfigPath is the current kubeconfig source path.
	KubeconfigPath string `json:"kubeconfigPath"`
	// ActiveContext is the resolved active context, when there is one.
	ActiveContext string `json:"activeContext,omitempty"`
	// CanSetKubeconfig reports whether the set-kubeconfig control is available
	// (flag on and not read-only).
	CanSetKubeconfig bool `json:"canSetKubeconfig"`
}

type setKubeconfigRequest struct {
	Path string `json:"path"`
}

// SetupStateHandler serves GET /api/v1/setup: the first-run / connectivity
// posture. It is registered unguarded (no read-only gate) and always answers
// 200 — the frontend gate depends on it even when no cluster is reachable.
// Probing respects the request context and is never cached. The active
// context's probe result is fed to onHealth (when non-nil), keeping the watch
// layer's reachability view fresh.
func SetupStateHandler(c Cluster, canSetKubeconfig bool, logger *slog.Logger, onHealth HealthObserver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, logger, http.StatusOK, resolveSetupState(c, canSetKubeconfig, r, onHealth))
	}
}

// SetKubeconfigHandler serves PUT /api/v1/kubeconfig: repoint the Manager at a
// different kubeconfig file at runtime (ADR-0007). It is registered inside the
// read-only guarded group, so read-only mode 403s it before this handler runs.
// The flag gate returns 403 when disabled; a malformed/relative path is 400; a
// candidate that fails validation is 422 with the previous source left intact;
// success returns the refreshed setup state.
func SetKubeconfigHandler(c Cluster, allowKubeconfigSet bool, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowKubeconfigSet {
			writeErrorGuidance(w, logger, http.StatusForbidden, "kubeconfig_set_disabled",
				"setting the kubeconfig source at runtime is disabled",
				"Set KUBESCOPE_ALLOW_KUBECONFIG_SET=true to enable pointing Kubescope at another kubeconfig at runtime (ADR-0007).")
			return
		}

		var req setKubeconfigRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSetKubeconfigBodyBytes))
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

		if err := c.SetKubeconfigPath(req.Path); err != nil {
			writeErrorGuidance(w, logger, http.StatusUnprocessableEntity, "kubeconfig_invalid",
				err.Error(),
				"The previous kubeconfig source is unchanged. The path must be absolute, readable by the "+
					"Kubescope process (in Docker: a mounted volume), and a valid kubeconfig with at least one context.")
			return
		}
		// The path is a mount point / operator-supplied location, not file
		// contents — safe to log; the kubeconfig itself is never logged.
		logger.Info("kubeconfig source switched", "path", req.Path)
		writeJSON(w, logger, http.StatusOK, resolveSetupState(c, allowKubeconfigSet, r, nil))
	}
}

// resolveSetupState derives the setup posture from the current kubeconfig and
// active-context reachability. It is shared by the setup endpoint and the
// set-kubeconfig success path so both report identical shape.
func resolveSetupState(c Cluster, canSetKubeconfig bool, r *http.Request, onHealth HealthObserver) SetupState {
	path := c.KubeconfigPath()
	st := SetupState{KubeconfigPath: path, CanSetKubeconfig: canSetKubeconfig}

	infos, err := c.Contexts()
	if err != nil {
		// The kubeconfig could not be read: distinguish "no file at all" from
		// "file present but unparseable" so the starter screen can tailor its help.
		if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
			st.State = "no_kubeconfig"
			st.Reason = "kubeconfig_missing"
			st.Message = err.Error()
			st.Guidance = fmt.Sprintf("No kubeconfig found at %s. Mount one into the container "+
				"(docker run -v ~/.kube/config:/kubeconfig:ro …) or point KUBESCOPE_KUBECONFIG at a readable file.", path)
			st.DocURL = docKubeconfigInDocker
			return st
		}
		st.State = "no_kubeconfig"
		st.Reason = "kubeconfig_invalid"
		st.Message = err.Error()
		st.Guidance = fmt.Sprintf("The file at %s is not a parseable kubeconfig — check it is valid YAML "+
			"with clusters, users and contexts, or point Kubescope at a different file.", path)
		return st
	}

	if len(infos) == 0 {
		st.State = "no_contexts"
		st.Guidance = "The kubeconfig defines no contexts — add one (kind export kubeconfig, " +
			"aws eks update-kubeconfig, …) or point Kubescope at a different file."
		return st
	}

	active, err := c.ActiveContextName()
	if err != nil {
		st.State = "no_active_context"
		st.Reason = "no_current_context"
		st.Guidance = "The kubeconfig has no current-context — pick a context to continue."
		return st
	}

	health := c.ProbeContext(r.Context(), active)
	if onHealth != nil {
		onHealth(health)
	}
	if !health.Reachable || !health.AuthOK {
		st.State = "active_unreachable"
		st.Reason = health.Reason
		st.Message = health.Error
		st.Guidance = health.Guidance
		st.DocURL = health.DocURL
		st.ActiveContext = active
		return st
	}

	st.State = "ready"
	st.ActiveContext = active
	return st
}
