package resources

import (
	"log/slog"
	"net/http"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// docKubeconfigInDocker is the auth/kubeconfig-in-Docker doc surfaced by the
// no-kubeconfig setup state; the same link classification.DocURL carries for
// connectivity failures (ADR-0004).
const docKubeconfigInDocker = "https://github.com/skriptvalley/kubescope/blob/main/docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md"

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
	// KubeconfigSources are the registered source paths in precedence order (the
	// registry itself, not the expanded file list) — the full listing lives at
	// GET /api/v1/kubeconfigs (ADR-0008).
	KubeconfigSources []string `json:"kubeconfigSources"`
	// ActiveContext is the resolved active context, when there is one.
	ActiveContext string `json:"activeContext,omitempty"`
	// CanSetKubeconfig reports whether the runtime source-registry controls are
	// available (flag on and not read-only).
	CanSetKubeconfig bool `json:"canSetKubeconfig"`
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

// resolveSetupState derives the setup posture from the current kubeconfig source
// registry and active-context reachability. It is shared by the setup endpoint
// and the source-registry mutation success paths so all report identical shape.
func resolveSetupState(c Cluster, canSetKubeconfig bool, r *http.Request, onHealth HealthObserver) SetupState {
	st := SetupState{KubeconfigSources: c.SourcePaths(), CanSetKubeconfig: canSetKubeconfig}

	infos, err := c.Contexts()
	if err != nil {
		// The registry resolved to no usable file (or the merge failed). Split
		// "nothing to read" (every source missing or empty) from "present but
		// unusable" using the per-source statuses so the starter screen can tailor
		// its help. os.Stat on a single path no longer applies — a source may be a
		// directory, and there may be several.
		st.State = "no_kubeconfig"
		st.Message = err.Error()
		if allSourcesMissingOrEmpty(c.Sources()) {
			st.Reason = "kubeconfig_missing"
			st.Guidance = "No usable kubeconfig found in the configured sources. Mount a kubeconfig file — " +
				"or a directory of kubeconfig files — into the container (docker run -v ~/.kube:/kubeconfigs:ro …), " +
				"then add it, or point KUBESCOPE_KUBECONFIG at a readable file or directory."
			st.DocURL = docKubeconfigInDocker
			return st
		}
		st.Reason = "kubeconfig_invalid"
		st.Guidance = "A configured kubeconfig source is present but unusable — check the files are valid YAML " +
			"with clusters, users and contexts, or point Kubescope at a different file or directory."
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
	// A canceled request yields a "context canceled" probe result that says
	// nothing about the cluster — never sync it into the watch layer.
	if onHealth != nil && r.Context().Err() == nil {
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

// allSourcesMissingOrEmpty reports whether every configured source is missing or
// empty (nothing to read) versus at least one being present-but-unusable
// (unparseable). A directory source reads as "empty" even when it holds broken
// files, so the per-file statuses are consulted too: an unparseable file inside
// it means the user supplied something that failed to register — that is
// kubeconfig_invalid territory, not "mount one". No sources configured at all
// counts as "nothing to read". The status literals mirror the kube.SourceStatus
// JSON contract (ADR-0008).
func allSourcesMissingOrEmpty(sources []kube.SourceStatus) bool {
	for _, s := range sources {
		if s.Status != "missing" && s.Status != "empty" {
			return false
		}
		for _, f := range s.Files {
			if f.Status == "unparseable" {
				return false
			}
		}
	}
	return true
}
