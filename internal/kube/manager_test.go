package kube

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// testCACert returns a valid self-signed CA certificate in PEM form, so
// clientset construction (which parses the CA) succeeds without a real cluster.
func testCACert(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kubescope-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func writeConfig(t *testing.T, cfg clientcmdapi.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(cfg, path))
	return path
}

// tokenAuth builds an embedded-token AuthInfo (the preferred, works-as-is form).
func tokenAuth() *clientcmdapi.AuthInfo { return &clientcmdapi.AuthInfo{Token: "embedded-token"} }

func TestContextsEnumeration(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"cluster-alpha": {Server: "https://alpha.example:6443", CertificateAuthorityData: ca},
			"cluster-beta":  {Server: "https://beta.example:6443", CertificateAuthorityData: ca},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"alpha": tokenAuth(), "beta": tokenAuth()},
		Contexts: map[string]*clientcmdapi.Context{
			"alpha": {Cluster: "cluster-alpha", AuthInfo: "alpha", Namespace: "team-a"},
			"beta":  {Cluster: "cluster-beta", AuthInfo: "beta"}, // no namespace -> default
		},
		CurrentContext: "beta",
	}
	m := NewManager(writeConfig(t, cfg))

	infos, err := m.Contexts()
	require.NoError(t, err)
	require.Equal(t, []ContextInfo{
		{Name: "alpha", Cluster: "cluster-alpha", Namespace: "team-a", Active: false},
		{Name: "beta", Cluster: "cluster-beta", Namespace: "default", Active: true},
	}, infos, "contexts sorted by name, active marked, namespace defaulted")

	active, err := m.ActiveContextName()
	require.NoError(t, err)
	assert.Equal(t, "beta", active)
}

func TestSwitchContext(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: ca}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts: map[string]*clientcmdapi.Context{
			"one": {Cluster: "c", AuthInfo: "u"},
			"two": {Cluster: "c", AuthInfo: "u"},
		},
		CurrentContext: "one",
	}
	m := NewManager(writeConfig(t, cfg))

	require.NoError(t, m.SwitchContext("two"))
	active, err := m.ActiveContextName()
	require.NoError(t, err)
	assert.Equal(t, "two", active)

	infos, err := m.Contexts()
	require.NoError(t, err)
	for _, i := range infos {
		assert.Equal(t, i.Name == "two", i.Active, "only the switched-to context is active")
	}

	var unknown *UnknownContextError
	assert.ErrorAs(t, m.SwitchContext("nope"), &unknown, "unknown context is a typed error")
	assert.Error(t, m.SwitchContext(""), "empty name rejected")

	// A rejected switch leaves the previous active context untouched.
	active, err = m.ActiveContextName()
	require.NoError(t, err)
	assert.Equal(t, "two", active)
}

func TestSwitchObserverNotifiedWithNewContext(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: ca}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts: map[string]*clientcmdapi.Context{
			"one": {Cluster: "c", AuthInfo: "u"},
			"two": {Cluster: "c", AuthInfo: "u"},
		},
		CurrentContext: "one",
	}
	m := NewManager(writeConfig(t, cfg))

	var got []string
	m.SetSwitchObserver(func(current string) { got = append(got, current) })

	require.NoError(t, m.SwitchContext("two"))
	assert.Equal(t, []string{"two"}, got, "observer sees the new active context")

	// A rejected switch must not notify — nothing was torn down.
	assert.Error(t, m.SwitchContext("nope"))
	assert.Equal(t, []string{"two"}, got, "a failed switch fires no observer")
}

func TestRestConfigForReturnsIndependentCopy(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: ca}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts:  map[string]*clientcmdapi.Context{"one": {Cluster: "c", AuthInfo: "u"}},
		// CurrentContext intentionally set so ActiveContextName resolves.
		CurrentContext: "one",
	}
	m := NewManager(writeConfig(t, cfg))

	rc, err := m.RestConfigFor("one")
	require.NoError(t, err)
	assert.Equal(t, "https://c:6443", rc.Host)

	// The returned config is a copy: mutating it (as the exec/port-forward paths
	// might) must not corrupt the cached config the shared clients use.
	rc.Timeout = 42 * time.Second
	rc2, err := m.RestConfigFor("one")
	require.NoError(t, err)
	assert.Zero(t, rc2.Timeout, "RestConfigFor must hand out an independent copy")
}

func TestSwitchContextNeverWritesKubeconfig(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: ca}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts: map[string]*clientcmdapi.Context{
			"one": {Cluster: "c", AuthInfo: "u"},
			"two": {Cluster: "c", AuthInfo: "u"},
		},
		CurrentContext: "one",
	}
	path := writeConfig(t, cfg)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	m := NewManager(path)
	require.NoError(t, m.SwitchContext("two"))

	// The #1 Sprint 1 invariant: the mounted kubeconfig is strictly read-only;
	// the switch lives only in memory.
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the mounted kubeconfig must never be written")

	reloaded, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "one", reloaded.CurrentContext, "current-context on disk stays unchanged after a switch")
}

func TestClientsetForCachesAndResolvesFilePathCA(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(caPath, testCACert(t), 0o600))

	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			// File-path CA reference (ADR-0004): resolves when the file is present.
			"c": {Server: "https://c:6443", CertificateAuthority: caPath},
		},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts:       map[string]*clientcmdapi.Context{"ctx": {Cluster: "c", AuthInfo: "u"}},
		CurrentContext: "ctx",
	}
	m := NewManager(writeConfig(t, cfg))

	cs1, err := m.ClientsetFor("ctx")
	require.NoError(t, err)
	cs2, err := m.Clientset() // active context -> same cached instance
	require.NoError(t, err)
	assert.Same(t, cs1, cs2, "clientset is built once and cached, never rebuilt per request")
}

func TestClientsetForMissingFilePathCA(t *testing.T) {
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"c": {Server: "https://c:6443", CertificateAuthority: "/does/not/exist/ca.crt"},
		},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts:       map[string]*clientcmdapi.Context{"ctx": {Cluster: "c", AuthInfo: "u"}},
		CurrentContext: "ctx",
	}
	m := NewManager(writeConfig(t, cfg))

	_, err := m.ClientsetFor("ctx")
	require.Error(t, err, "an unmounted file-path CA surfaces as an error, not a panic")
}

func TestMalformedKubeconfigIsStructuredError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte("this: is: not: valid: kubeconfig: ["), 0o600))
	m := NewManager(path)

	_, err := m.Contexts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading kubeconfig")

	_, err = m.ActiveContextName()
	require.Error(t, err)
}

func TestMissingKubeconfigIsStructuredError(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "absent"))
	_, err := m.Contexts()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading kubeconfig")
}

// singleContextConfig builds a minimal valid kubeconfig with one context.
func singleContextConfig(t *testing.T, ctxName string) clientcmdapi.Config {
	t.Helper()
	ca := testCACert(t)
	return clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: ca}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts:       map[string]*clientcmdapi.Context{ctxName: {Cluster: "c", AuthInfo: "u"}},
		CurrentContext: ctxName,
	}
}

func TestSetKubeconfigPathRejectsRelative(t *testing.T) {
	m := NewManager(writeConfig(t, singleContextConfig(t, "one")))
	require.Error(t, m.SetKubeconfigPath("relative/kubeconfig"), "a relative path is rejected")
	require.Error(t, m.SetKubeconfigPath(""), "an empty path is rejected")
}

func TestSetKubeconfigPathMissingFileLeavesSourceIntact(t *testing.T) {
	pathA := writeConfig(t, singleContextConfig(t, "alpha"))
	m := NewManager(pathA)

	err := m.SetKubeconfigPath(filepath.Join(t.TempDir(), "does-not-exist"))
	require.Error(t, err, "a missing candidate file is rejected")

	// The previous source is untouched — a typo never loses a working config.
	assert.Equal(t, pathA, m.KubeconfigPath())
	infos, cerr := m.Contexts()
	require.NoError(t, cerr)
	require.Len(t, infos, 1)
	assert.Equal(t, "alpha", infos[0].Name, "the prior kubeconfig still serves contexts")
}

func TestSetKubeconfigPathRejectsNoContexts(t *testing.T) {
	pathA := writeConfig(t, singleContextConfig(t, "alpha"))
	m := NewManager(pathA)

	empty := writeConfig(t, clientcmdapi.Config{})
	err := m.SetKubeconfigPath(empty)
	require.Error(t, err, "a kubeconfig with no contexts is rejected")
	assert.Contains(t, err.Error(), "no contexts")
	assert.Equal(t, pathA, m.KubeconfigPath(), "the prior source is unchanged")
}

func TestSetKubeconfigPathSwapsAndResetsState(t *testing.T) {
	pathA := writeConfig(t, clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: testCACert(t)}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts: map[string]*clientcmdapi.Context{
			"alpha": {Cluster: "c", AuthInfo: "u"},
			"beta":  {Cluster: "c", AuthInfo: "u"},
		},
		CurrentContext: "beta",
	})
	pathB := writeConfig(t, singleContextConfig(t, "gamma"))
	m := NewManager(pathA)

	// Set an active override and populate the per-context client cache.
	require.NoError(t, m.SwitchContext("alpha"))
	_, err := m.ClientsetFor("beta")
	require.NoError(t, err)
	m.mu.RLock()
	require.NotEmpty(t, m.clients, "cache populated before swap")
	m.mu.RUnlock()

	require.NoError(t, m.SetKubeconfigPath(pathB))

	assert.Equal(t, pathB, m.KubeconfigPath())
	infos, err := m.Contexts()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "gamma", infos[0].Name, "contexts reflect the new file immediately")

	active, err := m.ActiveContextName()
	require.NoError(t, err)
	assert.Equal(t, "gamma", active, "the stale active override was cleared; new current-context wins")

	m.mu.RLock()
	assert.Empty(t, m.clients, "the per-context client cache was reset")
	m.mu.RUnlock()
}

func TestProbeContext(t *testing.T) {
	cfg := clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"c": {Server: "https://127.0.0.1:1", CertificateAuthorityData: testCACert(t)}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts:       map[string]*clientcmdapi.Context{"ctx": {Cluster: "c", AuthInfo: "u"}},
		CurrentContext: "ctx",
	}
	m := NewManager(writeConfig(t, cfg))

	h := m.ProbeContext(context.Background(), "ctx")
	assert.Equal(t, "ctx", h.Name)
	assert.False(t, h.Reachable, "a closed loopback port refuses the connection")
	assert.Equal(t, string(FailConnectionRefused), h.Reason)
	assert.Contains(t, h.Guidance, "loopback", "loopback remediation is surfaced")

	unknown := m.ProbeContext(context.Background(), "nope")
	assert.Equal(t, "unknown context", unknown.Error)
	assert.Equal(t, string(FailUnknown), unknown.Reason)
	assert.False(t, unknown.Reachable)
}

func TestClassifyActiveErrorUsesActiveHints(t *testing.T) {
	cfg := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"c": {Server: "https://127.0.0.1:6443", CertificateAuthorityData: testCACert(t)}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"exec": {Exec: &clientcmdapi.ExecConfig{Command: "aws", APIVersion: "client.authentication.k8s.io/v1beta1"}},
		},
		Contexts:       map[string]*clientcmdapi.Context{"ctx": {Cluster: "c", AuthInfo: "exec"}},
		CurrentContext: "ctx",
	}
	m := NewManager(writeConfig(t, cfg))

	// Loopback hint drives connection-refused remediation for the active context.
	refused := m.ClassifyActiveError(errors.New("connect: connection refused"))
	assert.Equal(t, FailConnectionRefused, refused.Class)
	assert.Contains(t, refused.Remediation, "loopback")

	// Exec hint drives exec guidance on an otherwise opaque error.
	opaque := m.ClassifyActiveError(errors.New("some opaque failure"))
	assert.Equal(t, FailUnknown, opaque.Class)
	assert.Contains(t, opaque.Remediation, "ADR-0004")
}

func TestNoCurrentContext(t *testing.T) {
	ca := testCACert(t)
	cfg := clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"c": {Server: "https://c:6443", CertificateAuthorityData: ca}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts:  map[string]*clientcmdapi.Context{"solo": {Cluster: "c", AuthInfo: "u"}},
		// No current-context and no switch: listing works, no context is active.
	}
	m := NewManager(writeConfig(t, cfg))

	infos, err := m.Contexts()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.False(t, infos[0].Active, "with no current-context, nothing is marked active")

	_, err = m.ActiveContextName()
	require.Error(t, err, "no active context is a clear error, not a silent default")

	// Switching selects one explicitly.
	require.NoError(t, m.SwitchContext("solo"))
	active, err := m.ActiveContextName()
	require.NoError(t, err)
	assert.Equal(t, "solo", active)
}

// TestProbeEvictsStaleCachedClient covers the recreated-cluster case: a client
// cached against the old endpoint must be evicted once a probe succeeds against
// the kubeconfig's current server, so REST and stream paths stop dialing the
// dead endpoint (they rebuild on next use instead of failing until a restart).
func TestProbeEvictsStaleCachedClient(t *testing.T) {
	apiserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"33","gitVersion":"v1.33.0"}`))
	}))
	defer apiserver.Close()

	cfgAt := func(server string) clientcmdapi.Config {
		return clientcmdapi.Config{
			Clusters:       map[string]*clientcmdapi.Cluster{"c": {Server: server}},
			AuthInfos:      map[string]*clientcmdapi.AuthInfo{"u": {Token: "t"}},
			Contexts:       map[string]*clientcmdapi.Context{"ctx": {Cluster: "c", AuthInfo: "u"}},
			CurrentContext: "ctx",
		}
	}

	// The kubeconfig first names a dead endpoint; the client gets cached.
	path := writeConfig(t, cfgAt("http://127.0.0.1:1"))
	m := NewManager(path)
	_, err := m.ClientsetFor("ctx")
	require.NoError(t, err)
	m.mu.RLock()
	require.Contains(t, m.clients, "ctx", "client cached against the old endpoint")
	m.mu.RUnlock()

	// The cluster is "recreated" at a new address: same file, new server.
	require.NoError(t, clientcmd.WriteToFile(cfgAt(apiserver.URL), path))

	h := m.ProbeContext(context.Background(), "ctx")
	require.True(t, h.Reachable, "probe reaches the new endpoint: %s", h.Error)

	m.mu.RLock()
	assert.NotContains(t, m.clients, "ctx", "the stale cached client is evicted on a successful probe")
	m.mu.RUnlock()

	// Rebuilt on next use, against the current endpoint.
	cs, err := m.ClientsetFor("ctx")
	require.NoError(t, err)
	v, err := cs.Discovery().ServerVersion()
	require.NoError(t, err)
	assert.Equal(t, "v1.33.0", v.GitVersion)
}

// TestSetKubeconfigPathBumpsGenerationAndNotifies pins the source-swap contract
// (ADR-0007 + PR review): a successful swap increments SourceGeneration (so
// context-keyed caches key away from the old file's same-named contexts) and
// fires the source observer (so live exec/port-forward sessions are torn down);
// a failed swap does neither.
func TestSetKubeconfigPathBumpsGenerationAndNotifies(t *testing.T) {
	pathA := writeConfig(t, singleContextConfig(t, "alpha"))
	pathB := writeConfig(t, singleContextConfig(t, "beta"))
	m := NewManager(pathA)

	fired := 0
	m.SetSourceObserver(func() { fired++ })
	require.EqualValues(t, 0, m.SourceGeneration())

	require.Error(t, m.SetKubeconfigPath(filepath.Join(t.TempDir(), "missing")))
	assert.EqualValues(t, 0, m.SourceGeneration(), "a failed swap must not bump the generation")
	assert.Equal(t, 0, fired, "a failed swap must not notify")

	require.NoError(t, m.SetKubeconfigPath(pathB))
	assert.EqualValues(t, 1, m.SourceGeneration())
	assert.Equal(t, 1, fired, "a successful swap notifies exactly once")

	require.NoError(t, m.SetKubeconfigPath(pathA))
	assert.EqualValues(t, 2, m.SourceGeneration())
	assert.Equal(t, 2, fired)
}
