package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testUser = "operator"
	testPass = "s3cr3t-p@ss"
)

// authServer builds a router in the given auth mode. logBuf, when non-nil,
// captures log output so tests can assert credentials never appear in it.
func authServer(mode, user, pass string, logBuf *bytes.Buffer) http.Handler {
	var sink io.Writer = io.Discard
	if logBuf != nil {
		sink = logBuf
	}
	logger := slog.New(slog.NewTextHandler(sink, nil))
	return New(Options{
		Logger:            logger,
		Kube:              &fakeProvider{clientset: fake.NewClientset()},
		ReadOnly:          false,
		AuthMode:          mode,
		BasicAuthUsername: user,
		BasicAuthPassword: pass,
		Dist:              fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("spa")}},
	})
}

// authRoutes are the representative surfaces the auth matrix runs against: an API
// route, the UI-facing config endpoint, and the SPA fallback. /healthz is tested
// separately because it is exempt in every mode.
var authRoutes = []string{"/api/v1/nodes", "/api/v1/config", "/"}

func requestWithAuth(t *testing.T, srv http.Handler, path, user, pass string, withCreds bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if withCreds {
		r.SetBasicAuth(user, pass)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	return rec
}

// TestAuthNonePassesThrough: mode "none" gates nothing, with or without creds.
func TestAuthNonePassesThrough(t *testing.T) {
	srv := authServer("none", "", "", nil)
	for _, path := range append(authRoutes, "/healthz") {
		rec := requestWithAuth(t, srv, path, "", "", false)
		assert.NotEqual(t, http.StatusUnauthorized, rec.Code, "none mode must not 401 %s", path)
	}
}

// TestBasicRejectsMissingCredentials: every gated route 401s without credentials
// and advertises the Basic challenge so browsers prompt.
func TestBasicRejectsMissingCredentials(t *testing.T) {
	srv := authServer("basic", testUser, testPass, nil)
	for _, path := range authRoutes {
		rec := requestWithAuth(t, srv, path, "", "", false)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "basic mode must 401 %s without creds", path)
		assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Basic", "must challenge %s", path)
	}
}

// TestBasicAcceptsCorrectCredentials: correct credentials reach the handler.
func TestBasicAcceptsCorrectCredentials(t *testing.T) {
	srv := authServer("basic", testUser, testPass, nil)
	for _, path := range authRoutes {
		rec := requestWithAuth(t, srv, path, testUser, testPass, true)
		assert.NotEqual(t, http.StatusUnauthorized, rec.Code, "correct creds must pass %s (got %d)", path, rec.Code)
	}
}

// TestBasicRejectsWrongCredentials: wrong username or wrong password both 401.
func TestBasicRejectsWrongCredentials(t *testing.T) {
	srv := authServer("basic", testUser, testPass, nil)
	cases := []struct {
		name, user, pass string
	}{
		{"wrong password", testUser, "nope"},
		{"wrong username", "intruder", testPass},
		{"both wrong", "intruder", "nope"},
		{"empty password", testUser, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := requestWithAuth(t, srv, "/api/v1/config", tc.user, tc.pass, true)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

// TestHealthzExemptFromAuth: /healthz answers unauthenticated in basic mode too,
// so probes never need credentials.
func TestHealthzExemptFromAuth(t *testing.T) {
	srv := authServer("basic", testUser, testPass, nil)
	rec := requestWithAuth(t, srv, "/healthz", "", "", false)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ok")
}

// TestAuthNeverLogsCredentials: a rejected attempt must not leak the configured
// or submitted credentials into logs (ADR-0005: secrets are never logged).
func TestAuthNeverLogsCredentials(t *testing.T) {
	var buf bytes.Buffer
	srv := authServer("basic", testUser, testPass, &buf)

	// A wrong-credential attempt, which triggers the "auth rejected" log line.
	rec := requestWithAuth(t, srv, "/api/v1/config", "intruder", "leaked-guess", true)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	logs := buf.String()
	assert.NotContains(t, logs, testPass, "configured password must never be logged")
	assert.NotContains(t, logs, "leaked-guess", "submitted password must never be logged")
	assert.NotContains(t, logs, "intruder", "submitted username must never be logged")
	assert.Contains(t, logs, "auth rejected", "the rejection should still be logged")
}
