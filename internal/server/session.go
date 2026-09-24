package server

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	// a full PBKDF2 derivation rather than one HMAC (unless a session key is set,
	// which makes offline guessing hopeless).
	sessionKDFIterations = 600_000
	// Failed sign-ins are capped process-wide — not per client: behind a proxy the
	// client address is only as trustworthy as its headers. A burst of 10, then
	// one more every 6 s (10/min); successful sign-ins never consume the budget.
	signInFailBurst = 10
	signInFailEvery = 6 * time.Second
)

// signInFailDelay slows each failed guess a little further. A variable so tests
// can zero it.
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
	key        []byte // HMAC key for tokens, derived from the credential (+ session key)
	cookiePath string // the mount ("/" or "<base>/"), so the cookie stays on Kubescope's own path
	now        func() time.Time
	fails      *failLimiter
	logger     *slog.Logger
}

// newSessionAuth derives the token key once: PBKDF2-SHA256 over the password
// (salted with the username), mixed with the optional random session key.
// Deterministic, so sessions survive restarts; changing the credential or the
// session key ends every session.
func newSessionAuth(username, password, sessionKey, basePath string, logger *slog.Logger) *sessionAuth {
	dk, err := pbkdf2.Key(sha256.New, password, []byte("kubescope-session/v1\x00"+username), sessionKDFIterations, 32)
	if err != nil {
		panic("session key derivation: " + err.Error()) // only on invalid constant parameters
	}
	mac := hmac.New(sha256.New, []byte(sessionKey))
	mac.Write(dk)
	s := &sessionAuth{
		username:   username,
		userHash:   sha256.Sum256([]byte(username)),
		passHash:   sha256.Sum256([]byte(password)),
		key:        mac.Sum(nil),
		cookiePath: basePath + "/",
		now:        time.Now,
		logger:     logger,
	}
	s.fails = &failLimiter{burst: signInFailBurst, every: signInFailEvery, tokens: signInFailBurst, now: func() time.Time { return s.now() }}
	return s
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
// the canonical decimal expiry and never further out than one TTL; it returns
// the expiry so long-lived requests can be bounded by it.
func (s *sessionAuth) valid(tok string) (int64, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return 0, false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || parts[1] != strconv.FormatInt(exp, 10) {
		return 0, false
	}
	now := s.now()
	if now.Unix() >= exp || exp > now.Add(sessionTTL+time.Minute).Unix() {
		return 0, false
	}
	if !hmac.Equal([]byte(parts[2]), []byte(s.sign(exp))) {
		return 0, false
	}
	return exp, true
}

// session returns the expiry of the first valid session cookie. Every cookie of
// the name is tried: a stray one planted with a longer path (or a parent domain)
// is sent first and must not shadow the real one into a sign-in loop.
func (s *sessionAuth) session(r *http.Request) (int64, bool) {
	for _, c := range r.CookiesNamed(sessionCookieName) {
		if exp, ok := s.valid(c.Value); ok {
			return exp, true
		}
	}
	return 0, false
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
	_, ok := s.session(r)
	return SessionState{Mode: "session", Authenticated: ok, UsernameRequired: s.username != ""}
}

// guard admits /healthz, the session endpoint and every non-/api path (the SPA
// shell + assets, which render the sign-in page); any other /api request needs a
// valid session cookie, else a JSON 401 the SPA turns into the sign-in page. An
// admitted request's context ends when its token does, so a live stream or an
// exec shell can't outlive the session it was opened with.
func (s *sessionAuth) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == healthzPath || p == sessionRoute || !isAPIPath(p) {
			next.ServeHTTP(w, r)
			return
		}
		exp, ok := s.session(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthenticated", "sign in required")
			return
		}
		ctx, cancel := context.WithDeadline(r.Context(), time.Unix(exp, 0))
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
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
		Secure: r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		// Strict costs nothing here: every authenticated request is a same-origin
		// fetch from the (public) SPA shell, never a cross-site navigation.
		SameSite: http.SameSiteStrictMode,
	})
}

// sessionHandlers serves GET/POST/DELETE /api/v1/auth/session. In session mode
// GET reports the sign-in state, POST signs in and DELETE signs out. In the
// other modes GET reports "already authenticated" (the request got past Basic or
// no auth at all) and POST/DELETE are 404 — there is no session to manage. A
// session mode without its state (a wiring bug) fails closed.
func sessionHandlers(mode string, s *sessionAuth, logger *slog.Logger) (get, post, del http.HandlerFunc) {
	if s == nil {
		if mode == "session" {
			broken := func(w http.ResponseWriter, _ *http.Request) {
				writeJSONError(w, http.StatusServiceUnavailable, "auth_misconfigured", "session auth is not initialised")
			}
			return broken, broken, broken
		}
		notEnabled := func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusNotFound, "not_found", "session sign-in is not enabled (KUBESCOPE_AUTH_MODE="+mode+")")
		}
		get = func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, SessionState{Mode: mode, Authenticated: true})
		}
		return get, notEnabled, notEnabled
	}
	get = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.state(r))
	}
	post = func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := s.fails.peek(); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			writeJSONError(w, http.StatusTooManyRequests, "rate_limited", "too many failed sign-ins — wait a moment and try again")
			return
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "expected a JSON body with a password")
			return
		}
		if !s.credentialsMatch(body.Username, body.Password) {
			s.fails.spend()
			// Never log what was submitted — only that a sign-in failed.
			logger.Warn("sign-in rejected", "remote", r.RemoteAddr)
			time.Sleep(signInFailDelay)
			writeJSONError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
			return
		}
		s.setCookie(w, r, s.mint(), int(sessionTTL/time.Second))
		writeJSON(w, http.StatusOK, SessionState{Mode: "session", Authenticated: true, UsernameRequired: s.username != ""})
	}
	del = func(w http.ResponseWriter, r *http.Request) {
		s.setCookie(w, r, "", -1)
		writeJSON(w, http.StatusOK, SessionState{Mode: "session", Authenticated: false, UsernameRequired: s.username != ""})
	}
	return get, post, del
}

// failLimiter is a token bucket spent by failed sign-ins only.
type failLimiter struct {
	mu     sync.Mutex
	tokens float64
	burst  float64
	every  time.Duration
	last   time.Time
	now    func() time.Time
}

func (l *failLimiter) refill() {
	now := l.now()
	if !l.last.IsZero() {
		l.tokens = math.Min(l.burst, l.tokens+float64(now.Sub(l.last))/float64(l.every))
	}
	l.last = now
}

// peek reports whether a sign-in may be attempted, else how long until one may.
func (l *failLimiter) peek() (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	if l.tokens >= 1 {
		return true, 0
	}
	return false, time.Duration((1 - l.tokens) * float64(l.every))
}

// spend records one failed sign-in.
func (l *failLimiter) spend() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	l.tokens = math.Max(0, l.tokens-1)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
