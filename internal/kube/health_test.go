package kube

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
		hints         ClassifyHints
		wantReachable bool
		wantAuthOK    bool
		wantReason    FailureClass
		wantGuidance  bool
		guidanceHas   string // substring the guidance must contain, if any
	}{
		{
			name:          "unauthorized: server reachable, auth rejected",
			err:           apierrors.NewUnauthorized("bad token"),
			wantReachable: true,
			wantAuthOK:    false,
			wantReason:    FailAuthExpired,
			wantGuidance:  true,
		},
		{
			name:          "forbidden: server reachable, auth rejected",
			err:           apierrors.NewForbidden(gr, "x", errors.New("nope")),
			wantReachable: true,
			wantAuthOK:    false,
			wantReason:    FailForbidden,
			wantGuidance:  true,
		},
		{
			name:          "connection refused: unreachable, guidance from taxonomy",
			err:           errors.New("dial tcp 127.0.0.1:6443: connect: connection refused"),
			hints:         ClassifyHints{LoopbackServer: true},
			wantReachable: false,
			wantAuthOK:    false,
			wantReason:    FailConnectionRefused,
			wantGuidance:  true,
			guidanceHas:   "loopback",
		},
		{
			name:          "auth failure surfaced as plain string",
			err:           errors.New("the server has asked for the client to provide credentials (401 Unauthorized)"),
			wantReachable: true,
			wantAuthOK:    false,
			wantReason:    FailAuthExpired,
			wantGuidance:  true,
		},
		{
			name:          "opaque error on exec context still carries exec guidance",
			err:           errors.New(`exec: "aws": executable file not found in $PATH`),
			hints:         ClassifyHints{ExecCommand: "aws"},
			wantReachable: false,
			wantReason:    FailUnknown,
			wantGuidance:  true,
			guidanceHas:   "ADR-0004",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := classify(tt.err, tt.hints)
			assert.Equal(t, tt.wantReachable, h.Reachable)
			assert.Equal(t, tt.wantAuthOK, h.AuthOK)
			assert.NotEmpty(t, h.Error)
			assert.Equal(t, string(tt.wantReason), h.Reason)
			if tt.wantGuidance {
				assert.NotEmpty(t, h.Guidance)
			} else {
				assert.Empty(t, h.Guidance)
			}
			if tt.guidanceHas != "" {
				assert.Contains(t, h.Guidance, tt.guidanceHas)
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
	m := newManager(writeConfig(t, cfg))

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
	assert.Equal(t, string(FailConnectionRefused), tokenHealth.Reason, "refused connection classifies as connection_refused")
	assert.NotContains(t, tokenHealth.Guidance, "exec credential plugin", "non-exec context gets no exec guidance")
}

func TestProbeAllMalformedConfig(t *testing.T) {
	m := newManager(filepath.Join(t.TempDir(), "absent"))
	_, err := m.ProbeAll(context.Background())
	require.Error(t, err)
}

func TestExecGuidanceMethod(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: ca}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"exec":  {Exec: &clientcmdapi.ExecConfig{Command: "aws", APIVersion: "client.authentication.k8s.io/v1beta1"}},
			"token": tokenAuth(),
		},
		Contexts: map[string]*clientcmdapi.Context{
			"exec-ctx":  {Cluster: "c", AuthInfo: "exec"},
			"token-ctx": {Cluster: "c", AuthInfo: "token"},
		},
		CurrentContext: "token-ctx",
	}
	m := newManager(writeConfig(t, cfg))
	assert.Contains(t, m.ExecGuidance("exec-ctx"), "ADR-0004")
	assert.Empty(t, m.ExecGuidance("token-ctx"), "non-exec context yields no guidance")
	assert.Empty(t, m.ExecGuidance("missing"))
}

// blackHoleServer accepts TCP connections but never responds, so a client's TLS
// handshake stalls until the probe timeout fires. Returns its https:// URL.
func blackHoleServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	})
	return "https://" + ln.Addr().String()
}

// TestProbeAllConcurrentAndTimesOut proves two acceptance criteria at once:
// probes run concurrently (one unreachable context never blocks the other) and
// each is bounded by the per-probe timeout. Two black-hole servers each stall
// until the 500ms timeout; run concurrently they finish in ~500ms, run serially
// they would take ~1s.
func TestProbeAllConcurrentAndTimesOut(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"a": {Server: blackHoleServer(t), CertificateAuthorityData: ca},
			"b": {Server: blackHoleServer(t), CertificateAuthorityData: ca},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts: map[string]*clientcmdapi.Context{
			"ctx-a": {Cluster: "a", AuthInfo: "u"},
			"ctx-b": {Cluster: "b", AuthInfo: "u"},
		},
		CurrentContext: "ctx-a",
	}
	m := newManager(writeConfig(t, cfg))
	m.probeTimeout = 500 * time.Millisecond

	// Safety net: if the per-probe timeout were removed, the black-hole would
	// hang forever; this deadline makes ProbeAll return so the < 900ms
	// assertion fails cleanly instead of the test hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	health, err := m.ProbeAll(ctx)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.Len(t, health, 2)
	assert.Less(t, elapsed, 900*time.Millisecond, "probes must run concurrently and honor the per-probe timeout")
	for _, h := range health {
		assert.False(t, h.Reachable, "a black-hole server is unreachable")
		assert.NotEmpty(t, h.Error, "the timeout failure reason is surfaced")
	}
}
