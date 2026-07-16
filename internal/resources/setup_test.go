package resources

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	// invalidPath is a real file so os.Stat succeeds, letting the resolver
	// distinguish "present but unparseable" from "missing".
	invalidPath := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(invalidPath, []byte("not valid yaml: ["), 0o600))

	tests := []struct {
		name          string
		cluster       *fakeCluster
		canSet        bool
		wantState     string
		wantReason    string
		wantActive    string
		wantDocURL    bool
		wantGuidance  bool
		wantCanSetSet bool
	}{
		{
			name: "no kubeconfig when file is missing",
			cluster: &fakeCluster{
				kubeconfigPath: filepath.Join(t.TempDir(), "does-not-exist"),
				contextsErr:    errors.New("loading kubeconfig: no such file or directory"),
			},
			canSet:       true,
			wantState:    "no_kubeconfig",
			wantReason:   "kubeconfig_missing",
			wantDocURL:   true,
			wantGuidance: true,
		},
		{
			name: "no kubeconfig when file is unparseable",
			cluster: &fakeCluster{
				kubeconfigPath: invalidPath,
				contextsErr:    errors.New("loading kubeconfig: parse error"),
			},
			canSet:       true,
			wantState:    "no_kubeconfig",
			wantReason:   "kubeconfig_invalid",
			wantDocURL:   false,
			wantGuidance: true,
		},
		{
			name: "no contexts when kubeconfig is empty",
			cluster: &fakeCluster{
				kubeconfigPath: "/kubeconfig",
				contexts:       []kube.ContextInfo{},
			},
			canSet:       true,
			wantState:    "no_contexts",
			wantReason:   "",
			wantGuidance: true,
		},
		{
			name: "no active context when current-context is unset",
			cluster: &fakeCluster{
				kubeconfigPath: "/kubeconfig",
				contexts:       []kube.ContextInfo{{Name: "a"}},
				activeErr:      errors.New("kubeconfig has no current-context set"),
			},
			canSet:       true,
			wantState:    "no_active_context",
			wantReason:   "no_current_context",
			wantGuidance: true,
		},
		{
			name: "active unreachable surfaces health fields",
			cluster: &fakeCluster{
				kubeconfigPath: "/kubeconfig",
				contexts:       []kube.ContextInfo{{Name: "prod", Active: true}},
				active:         "prod",
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
				kubeconfigPath: "/kubeconfig",
				contexts:       []kube.ContextInfo{{Name: "prod", Active: true}},
				active:         "prod",
				probeHealth:    kube.ContextHealth{Name: "prod", Reachable: true, AuthOK: true, ServerVersion: "v1.33.0"},
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
			assert.Equal(t, tt.cluster.kubeconfigPath, st.KubeconfigPath)
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

func TestSetKubeconfigHandler(t *testing.T) {
	doPut := func(t *testing.T, cluster Cluster, allow bool, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/kubeconfig", strings.NewReader(body))
		SetKubeconfigHandler(cluster, allow, discardLogger())(rec, req)
		return rec
	}

	t.Run("flag off is 403 kubeconfig_set_disabled", func(t *testing.T) {
		cluster := &fakeCluster{kubeconfigPath: "/original"}
		rec := doPut(t, cluster, false, `{"path":"/new/kubeconfig"}`)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Equal(t, "kubeconfig_set_disabled", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.setPath, "the source must not be touched when the flag is off")
	})

	t.Run("malformed body is 400 invalid_request", func(t *testing.T) {
		cluster := &fakeCluster{kubeconfigPath: "/original"}
		rec := doPut(t, cluster, true, `not json`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.setPath)
	})

	t.Run("relative path is 400 invalid_request", func(t *testing.T) {
		cluster := &fakeCluster{kubeconfigPath: "/original"}
		rec := doPut(t, cluster, true, `{"path":"relative/kubeconfig"}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.setPath)
	})

	t.Run("invalid candidate is 422 and leaves the source intact", func(t *testing.T) {
		cluster := &fakeCluster{
			kubeconfigPath: "/original",
			setErr:         errors.New(`kubeconfig "/new/kubeconfig" defines no contexts`),
		}
		rec := doPut(t, cluster, true, `{"path":"/new/kubeconfig"}`)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, "kubeconfig_invalid", errorCode(t, rec.Body.Bytes()))
		assert.Equal(t, "/new/kubeconfig", cluster.setPath, "the swap was attempted")
		assert.Equal(t, "/original", cluster.kubeconfigPath, "the previous source is unchanged")
	})

	t.Run("success returns 200 with the refreshed setup state", func(t *testing.T) {
		cluster := &fakeCluster{
			kubeconfigPath: "/original",
			contexts:       []kube.ContextInfo{{Name: "prod", Active: true}},
			active:         "prod",
			probeHealth:    kube.ContextHealth{Name: "prod", Reachable: true, AuthOK: true},
		}
		rec := doPut(t, cluster, true, `{"path":"/new/kubeconfig"}`)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Equal(t, "/new/kubeconfig", cluster.kubeconfigPath, "the source was swapped")
		st := decodeSetupState(t, rec.Body.Bytes())
		assert.Equal(t, "ready", st.State)
		assert.Equal(t, "prod", st.ActiveContext)
		assert.Equal(t, "/new/kubeconfig", st.KubeconfigPath)
	})

	t.Run("unknown fields are rejected", func(t *testing.T) {
		cluster := &fakeCluster{kubeconfigPath: "/original"}
		rec := doPut(t, cluster, true, `{"path":"/new/kubeconfig","extra":true}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
	})
}

// TestSetupStateNotifiesHealthObserver pins the probe→stream signal: resolving
// the setup state feeds the active context's probe result to the observer, so
// the watch layer learns about outages the watch itself cannot see.
func TestSetupStateNotifiesHealthObserver(t *testing.T) {
	cluster := &fakeCluster{
		kubeconfigPath: "/kubeconfig",
		contexts:       []kube.ContextInfo{{Name: "prod", Active: true}},
		active:         "prod",
		probeHealth:    kube.ContextHealth{Name: "prod", Reason: "connection_refused", Error: "refused"},
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
