package resources

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skriptvalley/kubescope/internal/kube"
)

func decodeSetupState(t *testing.T, body []byte) SetupState {
	t.Helper()
	var st SetupState
	require.NoError(t, json.Unmarshal(body, &st))
	return st
}

func TestSetupStateHandler(t *testing.T) {
	tests := []struct {
		name         string
		cluster      *fakeCluster
		canSet       bool
		wantState    string
		wantReason   string
		wantActive   string
		wantDocURL   bool
		wantGuidance bool
	}{
		{
			// Every source missing/empty → "nothing to read" (kubeconfig_missing),
			// derived from the per-source statuses rather than an os.Stat.
			name: "no kubeconfig when every source is missing",
			cluster: &fakeCluster{
				sourcePaths: []string{"/kubeconfig"},
				sources:     []kube.SourceStatus{{Path: "/kubeconfig", Status: "missing"}},
				contextsErr: errors.New("no usable kubeconfig source among: /kubeconfig (missing)"),
			},
			canSet:       true,
			wantState:    "no_kubeconfig",
			wantReason:   "kubeconfig_missing",
			wantDocURL:   true,
			wantGuidance: true,
		},
		{
			// A source present but unparseable → "present but unusable"
			// (kubeconfig_invalid), no Docker doc link.
			name: "no kubeconfig when a source is present but unparseable",
			cluster: &fakeCluster{
				sourcePaths: []string{"/kubeconfig"},
				sources:     []kube.SourceStatus{{Path: "/kubeconfig", Status: "unparseable"}},
				contextsErr: errors.New("no usable kubeconfig source among: /kubeconfig (unparseable)"),
			},
			canSet:       true,
			wantState:    "no_kubeconfig",
			wantReason:   "kubeconfig_invalid",
			wantDocURL:   false,
			wantGuidance: true,
		},
		{
			// A directory source reads "empty" when none of its files are usable,
			// but a broken file inside it means the user supplied something that
			// failed to register — the reason must be kubeconfig_invalid, not
			// "mount one".
			name: "no kubeconfig when a directory holds only an unparseable file",
			cluster: &fakeCluster{
				sourcePaths: []string{"/kubeconfigs"},
				sources: []kube.SourceStatus{{
					Path:   "/kubeconfigs",
					Kind:   "dir",
					Status: "empty",
					Files:  []kube.SourceFileStatus{{Path: "/kubeconfigs/broken.yaml", Status: "unparseable"}},
				}},
				contextsErr: errors.New("no usable kubeconfig source among: /kubeconfigs (empty)"),
			},
			canSet:       true,
			wantState:    "no_kubeconfig",
			wantReason:   "kubeconfig_invalid",
			wantDocURL:   false,
			wantGuidance: true,
		},
		{
			name: "no contexts when the merged config is empty",
			cluster: &fakeCluster{
				sourcePaths: []string{"/kubeconfig"},
				contexts:    []kube.ContextInfo{},
			},
			canSet:       true,
			wantState:    "no_contexts",
			wantReason:   "",
			wantGuidance: true,
		},
		{
			name: "no active context when current-context is unset",
			cluster: &fakeCluster{
				sourcePaths: []string{"/kubeconfig"},
				contexts:    []kube.ContextInfo{{Name: "a"}},
				activeErr:   errors.New("kubeconfig has no current-context set"),
			},
			canSet:       true,
			wantState:    "no_active_context",
			wantReason:   "no_current_context",
			wantGuidance: true,
		},
		{
			name: "active unreachable surfaces health fields",
			cluster: &fakeCluster{
				sourcePaths: []string{"/kubeconfig"},
				contexts:    []kube.ContextInfo{{Name: "prod", Active: true}},
				active:      "prod",
				probeHealth: kube.ContextHealth{
					Name:     "prod",
					Reason:   "connection_refused",
					Guidance: "the cluster may be stopped",
					Error:    "connection refused",
					DocURL:   "https://example.test/doc",
				},
			},
			canSet:       true,
			wantState:    "active_unreachable",
			wantReason:   "connection_refused",
			wantActive:   "prod",
			wantDocURL:   true,
			wantGuidance: true,
		},
		{
			name: "ready when the active context is reachable",
			cluster: &fakeCluster{
				sourcePaths: []string{"/kubeconfig"},
				contexts:    []kube.ContextInfo{{Name: "prod", Active: true}},
				active:      "prod",
				probeHealth: kube.ContextHealth{Name: "prod", Reachable: true, AuthOK: true, ServerVersion: "v1.33.0"},
			},
			canSet:     false,
			wantState:  "ready",
			wantReason: "",
			wantActive: "prod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			SetupStateHandler(tt.cluster, tt.canSet, discardLogger(), nil)(
				rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup", nil))

			// The setup endpoint is always a 200 — it is the gate the UI depends on.
			require.Equal(t, http.StatusOK, rec.Code)
			st := decodeSetupState(t, rec.Body.Bytes())
			assert.Equal(t, tt.wantState, st.State)
			assert.Equal(t, tt.wantReason, st.Reason)
			assert.Equal(t, tt.wantActive, st.ActiveContext)
			assert.Equal(t, tt.cluster.sourcePaths, st.KubeconfigSources)
			assert.Equal(t, tt.canSet, st.CanSetKubeconfig)
			if tt.wantDocURL {
				assert.NotEmpty(t, st.DocURL)
			} else {
				assert.Empty(t, st.DocURL)
			}
			if tt.wantGuidance {
				assert.NotEmpty(t, st.Guidance)
			}
		})
	}
}

// TestSetupStateNotifiesHealthObserver pins the probe→stream signal: resolving
// the setup state feeds the active context's probe result to the observer, so
// the watch layer learns about outages the watch itself cannot see.
func TestSetupStateNotifiesHealthObserver(t *testing.T) {
	cluster := &fakeCluster{
		sourcePaths: []string{"/kubeconfig"},
		contexts:    []kube.ContextInfo{{Name: "prod", Active: true}},
		active:      "prod",
		probeHealth: kube.ContextHealth{Name: "prod", Reason: "connection_refused", Error: "refused"},
	}
	var seen []kube.ContextHealth
	rec := httptest.NewRecorder()
	SetupStateHandler(cluster, true, discardLogger(), func(h kube.ContextHealth) { seen = append(seen, h) })(
		rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, seen, 1)
	assert.Equal(t, "prod", seen[0].Name)
	assert.Equal(t, "connection_refused", seen[0].Reason)
}
