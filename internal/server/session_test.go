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
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
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
	// A cross-site form can't sign the browser in, or out: the CSRF guard runs first.
	rec := do(srv, http.MethodPost, sessionRoute, `{"password":"`+testPass+`"}`, nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Nil(t, sessionCookie(t, rec))
	rec = do(srv, http.MethodDelete, sessionRoute, "", nil, map[string]string{"Sec-Fetch-Site": "same-site"})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Nil(t, sessionCookie(t, rec))
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func isValid(s *sessionAuth, tok string) bool { _, ok := s.valid(tok); return ok }

func TestSessionTokenValidation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	s := newSessionAuth("", testPass, "", "", quietLogger())
	s.now = clock
	tok := s.mint()
	exp, ok := s.valid(tok)
	require.True(t, ok)
	assert.Equal(t, now.Add(sessionTTL).Unix(), exp)

	parts := strings.Split(tok, ".")
	far := now.Add(365 * 24 * time.Hour).Unix()
	other := newSessionAuth("", "another-password", "", "", quietLogger())
	other.now = clock
	otherUser := newSessionAuth("root", testPass, "", "", quietLogger())
	otherUser.now = clock
	keyed := newSessionAuth("", testPass, "some-session-key", "", quietLogger())
	keyed.now = clock
	for name, bad := range map[string]string{
		"empty":                  "",
		"wrong version":          "v2." + parts[1] + "." + parts[2],
		"non-canonical exp":      "v1.0" + parts[1] + "." + parts[2],
		"plus-signed exp":        "v1.+" + parts[1] + "." + parts[2],
		"far-future, signed":     "v1." + strconv.FormatInt(far, 10) + "." + s.sign(far),
		"other password":         other.mint(),
		"other username":         otherUser.mint(),
		"signed with a sess key": keyed.mint(),
		"truncated mac":          parts[0] + "." + parts[1] + "." + parts[2][:12],
	} {
		assert.False(t, isValid(s, bad), name)
	}
	assert.False(t, isValid(keyed, tok), "a session key changes the token key (no cross-instance tokens)")

	// Expiry is enforced server-side, independent of the cookie's MaxAge.
	now = now.Add(sessionTTL - time.Second)
	assert.True(t, isValid(s, tok))
	now = now.Add(2 * time.Second)
	assert.False(t, isValid(s, tok))

	// Deterministic: a restart with the same credential (and key) accepts existing tokens.
	now = time.Unix(1_800_000_000, 0)
	again := newSessionAuth("", testPass, "", "", quietLogger())
	again.now = clock
	assert.True(t, isValid(again, tok))
	keyedAgain := newSessionAuth("", testPass, "some-session-key", "", quietLogger())
	keyedAgain.now = clock
	assert.True(t, isValid(keyedAgain, keyed.mint()))
}

// A stray cookie of the same name (a longer path, a parent domain) is sent
// first; it must not shadow the real session into a sign-in loop.
func TestSessionAcceptsAnyValidCookie(t *testing.T) {
	s := newSessionAuth("", testPass, "", "", quietLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	req.Header.Add("Cookie", sessionCookieName+"=v1.123.planted; "+sessionCookieName+"="+s.mint())
	_, ok := s.session(req)
	assert.True(t, ok)
}

// An admitted request's context ends when its token does, so a stream or an
// exec shell can't outlive the session it was opened with.
func TestSessionGuardBoundsRequestsByTokenExpiry(t *testing.T) {
	s := newSessionAuth("", testPass, "", "", quietLogger())
	tok := s.mint()
	exp, _ := s.valid(tok)
	var deadline time.Time
	var hasDeadline bool
	h := s.guard(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, hasDeadline = r.Context().Deadline()
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stream/resources/core/v1/pods", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	h.ServeHTTP(httptest.NewRecorder(), req)
	require.True(t, hasDeadline)
	assert.Equal(t, exp, deadline.Unix())
}

// Failed sign-ins are capped process-wide: parallel guessing hits a 429 with a
// Retry-After, successful sign-ins don't spend the budget, and it refills.
func TestSessionSignInFailureLimit(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := newSessionAuth("", testPass, "", "", quietLogger())
	s.now = func() time.Time { return now }
	_, post, _ := sessionHandlers("session", s, quietLogger())
	try := func(pw string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		post(rec, httptest.NewRequest(http.MethodPost, sessionRoute, strings.NewReader(`{"password":"`+pw+`"}`)))
		return rec
	}
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusOK, try(testPass).Code, "successes don't spend the budget")
	}
	for i := 0; i < signInFailBurst; i++ {
		require.Equal(t, http.StatusUnauthorized, try("guess").Code, "attempt %d", i)
	}
	rec := try(testPass)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "the budget is exhausted — even the right password waits")
	assert.Contains(t, rec.Body.String(), "rate_limited")
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))

	now = now.Add(signInFailEvery)
	assert.Equal(t, http.StatusOK, try(testPass).Code, "one attempt refilled")
}

func TestSessionMisconfigurationFailsClosed(t *testing.T) {
	h := authGuard("session", "", testPass, nil, quietLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	for _, p := range []string{"/api/v1/nodes", "/", sessionRoute} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, p)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code, "probes still pass")

	get, post, del := sessionHandlers("session", nil, quietLogger())
	for _, hf := range []http.HandlerFunc{get, post, del} {
		rec := httptest.NewRecorder()
		hf(rec, httptest.NewRequest(http.MethodGet, sessionRoute, nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	}
}

func TestAPIResponsesAreNotStored(t *testing.T) {
	srv := sessionServer(t, "", testPass, "", nil)
	rec := do(srv, http.MethodPost, sessionRoute, `{"password":"`+testPass+`"}`, nil, nil)
	c := sessionCookie(t, rec)
	for _, p := range []string{sessionRoute, "/api/v1/nodes", "/api/nope"} {
		assert.Equal(t, "no-store", do(srv, http.MethodGet, p, "", nil, nil).Header().Get("Cache-Control"), "signed out "+p)
		assert.Equal(t, "no-store", do(srv, http.MethodGet, p, "", c, nil).Header().Get("Cache-Control"), "signed in "+p)
	}
	assert.NotEqual(t, "no-store", do(srv, http.MethodGet, "/", "", nil, nil).Header().Get("Cache-Control"), "the shell keeps its own no-cache")
}

func TestIsLoopbackBind(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true, "localhost:8080": true, "[::1]:8080": true, "": true,
		"0.0.0.0:8080": false, ":8080": false, "10.0.0.5:8080": false, "[::]:8080": false,
	} {
		assert.Equal(t, want, isLoopbackBind(addr), addr)
	}
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

		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			req = httptest.NewRequest(method, sessionRoute, strings.NewReader(`{"password":"x"}`))
			req.SetBasicAuth(testUser, testPass)
			rec = httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusNotFound, rec.Code, mode+" "+method)
		}
	}
}
