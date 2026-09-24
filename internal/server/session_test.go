package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func init() { signInFailDelay = 0 } // keep the failure brake out of test runtime

func sessionServer(t *testing.T, user, pass, base string, logBuf *bytes.Buffer) http.Handler {
	t.Helper()
	var sink io.Writer = io.Discard
	if logBuf != nil {
		sink = logBuf
	}
	return New(Options{
		Logger:            slog.New(slog.NewTextHandler(sink, nil)),
		Kube:              &fakeProvider{clientset: fake.NewClientset()},
		AuthMode:          "session",
		BasicAuthUsername: user,
		BasicAuthPassword: pass,
		BasePath:          base,
		Dist:              fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html><head></head>spa</html>")}},
	})
}

func do(srv http.Handler, method, path, body string, cookie *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

func decodeState(t *testing.T, rec *httptest.ResponseRecorder) SessionState {
	t.Helper()
	var st SessionState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &st))
	return st
}

func TestSessionModeGatesTheAPI(t *testing.T) {
	var logs bytes.Buffer
	srv := sessionServer(t, "", testPass, "", &logs)

	// Signed out: the SPA shell, health and the session state are public; the API is not.
	assert.Equal(t, http.StatusOK, do(srv, http.MethodGet, "/", "", nil, nil).Code, "the shell renders the sign-in page")
	assert.Equal(t, http.StatusOK, do(srv, http.MethodGet, "/resources/core/v1/pods", "", nil, nil).Code)
	assert.Equal(t, http.StatusOK, do(srv, http.MethodGet, "/healthz", "", nil, nil).Code)
	st := decodeState(t, do(srv, http.MethodGet, sessionRoute, "", nil, nil))
	assert.Equal(t, SessionState{Mode: "session", Authenticated: false, UsernameRequired: false}, st)
	for _, p := range []string{"/api/v1/nodes", "/api/v1/config", "/api/v1/secrets/ns/s/reveal?key=k", "/api/nope"} {
		rec := do(srv, http.MethodGet, p, "", nil, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, p)
		assert.Contains(t, rec.Body.String(), `"unauthenticated"`, p)
	}

	// A wrong password is refused and never logged.
	rec := do(srv, http.MethodPost, sessionRoute, `{"password":"guess-123"}`, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_credentials")
	assert.Nil(t, sessionCookie(t, rec))
	assert.NotContains(t, logs.String(), "guess-123")

	// The right password signs in: an HttpOnly, Lax, 12h cookie on the mount path.
	rec = do(srv, http.MethodPost, sessionRoute, `{"password":"`+testPass+`"}`, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	c := sessionCookie(t, rec)
	require.NotNil(t, c)
	assert.True(t, c.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
	assert.Equal(t, int(sessionTTL/time.Second), c.MaxAge)
	assert.False(t, c.Secure, "plain http without a TLS proxy")
	assert.NotContains(t, logs.String(), testPass)

	// With the cookie the API answers and the state flips.
	assert.Equal(t, http.StatusOK, do(srv, http.MethodGet, "/api/v1/nodes", "", c, nil).Code)
	assert.True(t, decodeState(t, do(srv, http.MethodGet, sessionRoute, "", c, nil)).Authenticated)

	// Sign-out expires the cookie.
	rec = do(srv, http.MethodDelete, sessionRoute, "", c, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	out := sessionCookie(t, rec)
	require.NotNil(t, out)
	assert.Equal(t, -1, out.MaxAge)
	assert.Equal(t, "", out.Value)
}

func TestSessionCookieFollowsMountAndTLS(t *testing.T) {
	srv := sessionServer(t, "", testPass, "/kubescope", nil)
	// Behind the proxy the prefix is stripped or kept — either way the cookie is
	// scoped to the mount and Secure when the browser came over TLS.
	for _, path := range []string{sessionRoute, "/kubescope" + sessionRoute} {
		rec := do(srv, http.MethodPost, path, `{"password":"`+testPass+`"}`, nil, map[string]string{"X-Forwarded-Proto": "https"})
		require.Equal(t, http.StatusOK, rec.Code, path)
		c := sessionCookie(t, rec)
		require.NotNil(t, c, path)
		assert.Equal(t, "/kubescope/", c.Path, path)
		assert.True(t, c.Secure, path)
		assert.Equal(t, http.StatusOK, do(srv, http.MethodGet, "/kubescope/api/v1/nodes", "", c, nil).Code)
	}
}

func TestSessionUsernameWhenConfigured(t *testing.T) {
	srv := sessionServer(t, testUser, testPass, "", nil)
	assert.True(t, decodeState(t, do(srv, http.MethodGet, sessionRoute, "", nil, nil)).UsernameRequired)
	for name, body := range map[string]string{
		"no username":    `{"password":"` + testPass + `"}`,
		"wrong username": `{"username":"root","password":"` + testPass + `"}`,
		"wrong password": `{"username":"` + testUser + `","password":"nope"}`,
	} {
		assert.Equal(t, http.StatusUnauthorized, do(srv, http.MethodPost, sessionRoute, body, nil, nil).Code, name)
	}
	rec := do(srv, http.MethodPost, sessionRoute, `{"username":"`+testUser+`","password":"`+testPass+`"}`, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSessionSignInRejectsBadBodiesAndCrossSite(t *testing.T) {
	srv := sessionServer(t, "", testPass, "", nil)
	assert.Equal(t, http.StatusBadRequest, do(srv, http.MethodPost, sessionRoute, `not json`, nil, nil).Code)
	// A cross-site form can't sign the browser in (or out): the CSRF guard runs first.
	rec := do(srv, http.MethodPost, sessionRoute, `{"password":"`+testPass+`"}`, nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Nil(t, sessionCookie(t, rec))
}

func TestSessionTokenValidation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := newSessionAuth("", testPass, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return now }
	tok := s.mint()
	require.True(t, s.valid(tok))

	parts := strings.Split(tok, ".")
	far := now.Add(365 * 24 * time.Hour).Unix()
	other := newSessionAuth("", "another-password", "", s.logger)
	other.now = s.now
	for name, bad := range map[string]string{
		"empty":             "",
		"wrong version":     "v2." + parts[1] + "." + parts[2],
		"non-canonical exp": "v1.0" + parts[1] + "." + parts[2],
		"plus-signed exp":   "v1.+" + parts[1] + "." + parts[2],
		"extended expiry":   "v1.9999999999." + parts[2],
		"far-future signed": "v1." + strconv.FormatInt(far, 10) + "." + s.sign(far),
		"other credential":  other.mint(),
		"truncated mac":     parts[0] + "." + parts[1] + "." + parts[2][:12],
	} {
		assert.False(t, s.valid(bad), name)
	}

	// Expiry is enforced server-side, independent of the cookie's MaxAge.
	now = now.Add(sessionTTL - time.Second)
	assert.True(t, s.valid(tok))
	now = now.Add(2 * time.Second)
	assert.False(t, s.valid(tok))

	// Deterministic: a restart with the same credential accepts existing tokens.
	again := newSessionAuth("", testPass, "", s.logger)
	again.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	assert.True(t, again.valid(tok))
}

func TestSessionEndpointOutsideSessionMode(t *testing.T) {
	for _, mode := range []string{"none", "basic"} {
		srv := authServer(mode, testUser, testPass, nil)
		req := httptest.NewRequest(http.MethodGet, sessionRoute, nil)
		req.SetBasicAuth(testUser, testPass)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, mode)
		assert.Equal(t, SessionState{Mode: mode, Authenticated: true}, decodeState(t, rec), mode)

		req = httptest.NewRequest(http.MethodPost, sessionRoute, strings.NewReader(`{"password":"x"}`))
		req.SetBasicAuth(testUser, testPass)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code, mode)
	}
}
