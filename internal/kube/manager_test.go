package kube

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
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
