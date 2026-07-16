package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"
)

func hostGuardServer(listenAddr string) http.Handler {
	return New(Options{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:       &fakeProvider{clientset: fake.NewClientset()},
		ListenAddr: listenAddr,
		Dist:       fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("spa")}},
	})
}

func getWithHost(t *testing.T, srv http.Handler, path, host string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Host = host
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

// TestHostGuardLoopbackBind: a loopback bind allows loopback/localhost Hosts and
// rejects a rebinding attacker Host — while /healthz stays reachable regardless.
func TestHostGuardLoopbackBind(t *testing.T) {
	srv := hostGuardServer("127.0.0.1:8080")
	cases := []struct {
		name     string
		host     string
		wantCode int
	}{
		{"localhost with port", "localhost:8080", http.StatusOK},
		{"loopback ip with port", "127.0.0.1:8080", http.StatusOK},
		{"localhost no port", "localhost", http.StatusOK},
		{"loopback no port", "127.0.0.1", http.StatusOK},
		{"rebinding attacker host", "attacker.example", http.StatusForbidden},
		{"attacker host with loopback port", "attacker.example:8080", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := getWithHost(t, srv, "/api/v1/config", tc.host)
			assert.Equal(t, tc.wantCode, rec.Code)
			if tc.wantCode == http.StatusForbidden {
				assert.Contains(t, rec.Body.String(), "forbidden_host")
			}
		})
	}
}

// TestHostGuardHealthzExempt: probes reach /healthz even with a foreign Host.
func TestHostGuardHealthzExempt(t *testing.T) {
	srv := hostGuardServer("127.0.0.1:8080")
	rec := getWithHost(t, srv, "/healthz", "attacker.example")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestHostGuardConcreteBindAddsHost: binding an explicit address keeps that
// address reachable while still rejecting foreign Hosts.
func TestHostGuardConcreteBindAddsHost(t *testing.T) {
	srv := hostGuardServer("192.168.1.5:8080")
	assert.Equal(t, http.StatusOK, getWithHost(t, srv, "/api/v1/config", "192.168.1.5:8080").Code)
	assert.Equal(t, http.StatusOK, getWithHost(t, srv, "/api/v1/config", "localhost:8080").Code)
	assert.Equal(t, http.StatusForbidden, getWithHost(t, srv, "/api/v1/config", "evil.test").Code)
}

// TestHostGuardWildcardBindPassesThrough: a 0.0.0.0 bind (Docker default) has no
// enforceable allowlist, so any Host is accepted — protection is auth + network
// controls in that mode (ADR-0005).
func TestHostGuardWildcardBindPassesThrough(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", "[::]:8080", ""} {
		srv := hostGuardServer(addr)
		rec := getWithHost(t, srv, "/api/v1/config", "anything.example")
		assert.Equal(t, http.StatusOK, rec.Code, "wildcard bind %q must not enforce Host", addr)
	}
}

// TestAllowedHosts documents the allowlist derivation directly.
func TestAllowedHosts(t *testing.T) {
	assert.Nil(t, allowedHosts("0.0.0.0:8080"))
	assert.Nil(t, allowedHosts("[::]:8080"))
	assert.Nil(t, allowedHosts("garbage-no-port"))

	loop := allowedHosts("127.0.0.1:8080")
	assert.True(t, loop["localhost"] && loop["127.0.0.1"] && loop["::1"])

	concrete := allowedHosts("10.0.0.9:9090")
	assert.True(t, concrete["10.0.0.9"], "concrete bind host is allowlisted")
	assert.True(t, concrete["localhost"], "loopback names always allowed")
}
