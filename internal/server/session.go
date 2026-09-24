package server

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Session auth (KUBESCOPE_AUTH_MODE=session, ADR-0013): the operator signs in on
// a page instead of the browser's Basic prompt, and gets a stateless, expiring,
// HttpOnly cookie instead of re-sending credentials on every request. The SPA
// shell and its assets stay public (they hold no data and render the sign-in
// page); every /api route except the session endpoint itself needs the cookie.

const (
	sessionCookieName = "kubescope_session"
	sessionTTL        = 12 * time.Hour
	sessionRoute      = "/api/v1/auth/session"
	// sessionKDFIterations makes each password guess against a leaked cookie cost
	// a full PBKDF2 derivation rather than one HMAC.
	sessionKDFIterations = 600_000
)

// signInFailDelay slows password guessing through the sign-in endpoint. A
// variable so tests can zero it.
var signInFailDelay = 400 * time.Millisecond

// SessionState is the GET/POST /api/v1/auth/session body the SPA gates on.
type SessionState struct {
	Mode             string `json:"mode"`
	Authenticated    bool   `json:"authenticated"`
	UsernameRequired bool   `json:"usernameRequired"`
}

type sessionAuth struct {
	username   string // "" = password-only sign-in
	userHash   [32]byte
	passHash   [32]byte
	key        []byte // HMAC key for tokens, derived from the credential
	cookiePath string // the mount ("/" or "<base>/"), so the cookie stays on Kubescope's own path
	now        func() time.Time
	logger     *slog.Logger
}

// newSessionAuth derives the token key once: PBKDF2-SHA256 over the password,
// salted with the username. Deterministic, so sessions survive restarts; a
// credential change ends every session.
func newSessionAuth(username, password, basePath string, logger *slog.Logger) *sessionAuth {
	key, err := pbkdf2.Key(sha256.New, password, []byte("kubescope-session/v1\x00"+username), sessionKDFIterations, 32)
	if err != nil {
		panic("session key derivation: " + err.Error()) // only on invalid constant parameters
	}
	return &sessionAuth{
		username:   username,
		userHash:   sha256.Sum256([]byte(username)),
		passHash:   sha256.Sum256([]byte(password)),
		key:        key,
		cookiePath: basePath + "/",
		now:        time.Now,
		logger:     logger,
	}
}

func (s *sessionAuth) sign(exp int64) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("v1." + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// mint returns a token "v1.<expiry unix>.<hex hmac>".
func (s *sessionAuth) mint() string {
	exp := s.now().Add(sessionTTL).Unix()
	return "v1." + strconv.FormatInt(exp, 10) + "." + s.sign(exp)
}

// valid accepts only a well-formed, unexpired token signed with this key, with
// the canonical decimal expiry and never further out than one TTL.
func (s *sessionAuth) valid(tok string) bool {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || parts[1] != strconv.FormatInt(exp, 10) {
		return false
	}
	now := s.now()
	if now.Unix() >= exp || exp > now.Add(sessionTTL+time.Minute).Unix() {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(s.sign(exp)))
}

func (s *sessionAuth) authenticated(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	return err == nil && s.valid(c.Value)
}

// credentialsMatch compares in constant time (SHA-256 of each side, so neither
// value nor length leaks), evaluating both halves unconditionally. With no
// configured username only the password is checked.
func (s *sessionAuth) credentialsMatch(username, password string) bool {
	gotPass := sha256.Sum256([]byte(password))
	passOK := subtle.ConstantTimeCompare(gotPass[:], s.passHash[:]) == 1
	userOK := true
	if s.username != "" {
		gotUser := sha256.Sum256([]byte(username))
		userOK = subtle.ConstantTimeCompare(gotUser[:], s.userHash[:]) == 1
	}
	return userOK && passOK
}

func (s *sessionAuth) state(r *http.Request) SessionState {
	return SessionState{Mode: "session", Authenticated: s.authenticated(r), UsernameRequired: s.username != ""}
}

// guard admits /healthz, the session endpoint and every non-/api path (the SPA
// shell + assets, which render the sign-in page); any other /api request needs a
// valid session cookie, else a JSON 401 the SPA turns into the sign-in page.
func (s *sessionAuth) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == healthzPath || p == sessionRoute || !isAPIPath(p) || s.authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated", "sign in required")
	})
}

func isAPIPath(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }

func (s *sessionAuth) setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     s.cookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		// Secure whenever the browser reached us over TLS — directly or through a
		// TLS-terminating proxy — while still working on a plain-http localhost run.
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	})
}

// sessionHandlers serves GET/POST/DELETE /api/v1/auth/session. In session mode
// GET reports the sign-in state, POST signs in and DELETE signs out. In the
// other modes GET reports "already authenticated" (the request got past Basic or
// no auth at all) and POST/DELETE are 404 — there is no session to manage.
func sessionHandlers(mode string, s *sessionAuth, logger *slog.Logger) (get, post, del http.HandlerFunc) {
	notEnabled := func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusNotFound, "not_found", "session sign-in is not enabled (KUBESCOPE_AUTH_MODE="+mode+")")
	}
	if s == nil {
		get = func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, SessionState{Mode: mode, Authenticated: true})
		}
		return get, notEnabled, notEnabled
	}
	get = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, s.state(r))
	}
	post = func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "expected a JSON body with a password")
			return
		}
		if !s.credentialsMatch(body.Username, body.Password) {
			// Never log what was submitted — only that a sign-in failed, and from where.
			logger.Warn("sign-in rejected", "remote", r.RemoteAddr)
			time.Sleep(signInFailDelay)
			writeJSONError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
			return
		}
		s.setCookie(w, r, s.mint(), int(sessionTTL/time.Second))
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, SessionState{Mode: "session", Authenticated: true, UsernameRequired: s.username != ""})
	}
	del = func(w http.ResponseWriter, r *http.Request) {
		s.setCookie(w, r, "", -1)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, SessionState{Mode: "session", Authenticated: false, UsernameRequired: s.username != ""})
	}
	return get, post, del
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
