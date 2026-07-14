package kube

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestClassify(t *testing.T) {
	gr := schema.GroupResource{Resource: "nodes"}
	tests := []struct {
		name          string
		err           error
		usesExec      bool
		wantReachable bool
		wantAuthOK    bool
		wantGuidance  bool
	}{
		{
			name:          "unauthorized: server reachable, auth rejected",
			err:           apierrors.NewUnauthorized("bad token"),
			wantReachable: true,
			wantAuthOK:    false,
		},
		{
			name:          "forbidden: server reachable, auth rejected",
			err:           apierrors.NewForbidden(gr, "x", errors.New("nope")),
			wantReachable: true,
			wantAuthOK:    false,
		},
		{
			name:          "connection refused: unreachable",
			err:           errors.New("dial tcp 127.0.0.1:6443: connect: connection refused"),
			wantReachable: false,
			wantAuthOK:    false,
		},
		{
			name:          "auth failure surfaced as plain string",
			err:           errors.New("the server has asked for the client to provide credentials (401 Unauthorized)"),
			wantReachable: true,
			wantAuthOK:    false,
		},
		{
			name:          "exec context always carries guidance",
			err:           errors.New(`exec: "aws": executable file not found in $PATH`),
			usesExec:      true,
			wantReachable: false,
			wantGuidance:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := classify(tt.err, tt.usesExec, "aws")
			assert.Equal(t, tt.wantReachable, h.Reachable)
			assert.Equal(t, tt.wantAuthOK, h.AuthOK)
			assert.NotEmpty(t, h.Error)
			if tt.wantGuidance {
				assert.Contains(t, h.Guidance, "ADR-0004")
			} else {
				assert.Empty(t, h.Guidance)
			}
		})
	}
}

func TestExecGuidance(t *testing.T) {
	g := execGuidance("aws")
	assert.Contains(t, g, "aws")
	assert.Contains(t, g, "ADR-0004")
	assert.Contains(t, g, "token")
	assert.Contains(t, execGuidance(""), "credential plugin", "empty command still yields guidance")
}

func TestContextUsesExec(t *testing.T) {
	raw := clientcmdapi.Config{
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"exec-user":  {Exec: &clientcmdapi.ExecConfig{Command: "aws", APIVersion: "client.authentication.k8s.io/v1beta1"}},
			"token-user": {Token: "t"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"exec-ctx":  {AuthInfo: "exec-user"},
			"token-ctx": {AuthInfo: "token-user"},
		},
	}
	assert.True(t, contextUsesExec(raw, "exec-ctx"))
	assert.Equal(t, "aws", execCommand(raw, "exec-ctx"))
	assert.False(t, contextUsesExec(raw, "token-ctx"))
	assert.False(t, contextUsesExec(raw, "missing"))
}

func TestProbeAllExecFailureCarriesGuidance(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"c": {Server: "https://127.0.0.1:1", CertificateAuthorityData: ca},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"exec": {Exec: &clientcmdapi.ExecConfig{
				Command:         "kubescope-nonexistent-credential-plugin",
				APIVersion:      "client.authentication.k8s.io/v1beta1",
				InteractiveMode: clientcmdapi.NeverExecInteractiveMode,
			}},
			"token": {Token: "t"},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"exec-ctx":  {Cluster: "c", AuthInfo: "exec"},
			"token-ctx": {Cluster: "c", AuthInfo: "token"},
		},
		CurrentContext: "token-ctx",
	}
	m := NewManager(writeConfig(t, cfg))

	health, err := m.ProbeAll(context.Background())
	require.NoError(t, err)
	require.Len(t, health, 2)

	byName := map[string]ContextHealth{}
	for _, h := range health {
		byName[h.Name] = h
	}

	execHealth := byName["exec-ctx"]
	assert.False(t, execHealth.Reachable, "missing exec plugin is unreachable")
	assert.NotEmpty(t, execHealth.Guidance, "exec failure surfaces ADR-0004 guidance")
	assert.Contains(t, execHealth.Guidance, "ADR-0004")

	tokenHealth := byName["token-ctx"]
	assert.False(t, tokenHealth.Reachable, "127.0.0.1:1 refuses the connection")
	assert.Empty(t, tokenHealth.Guidance, "non-exec context gets no exec guidance")
}

func TestProbeAllMalformedConfig(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "absent"))
	_, err := m.ProbeAll(context.Background())
	require.Error(t, err)
}
