package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wera/internal/auth"
	"wera/internal/store"
)

// sessionCookie is the name of the login cookie.
const sessionCookie = "wera_session"

// sessionTTL is how long a login lasts.
const sessionTTL = 30 * 24 * time.Hour

type ctxKey int

const userKey ctxKey = iota

// currentUser returns the logged-in user set by requireUser.
func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

// sessionHash returns the hash of the request's session cookie, or "".
func sessionHash(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	return auth.HashToken(c.Value)
}

// requireUser rejects requests without a live session (401).
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := sessionHash(r)
		if h == "" {
			s.writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		u, err := store.SessionUser(r.Context(), s.Pool, h)
		if errors.Is(err, pgx.ErrNoRows) {
			s.clearSessionCookie(w)
			s.writeError(w, http.StatusUnauthorized, "session expired; log in again")
			return
		}
		if err != nil {
			s.Log.Error("session lookup failed", "err", err)
			s.writeError(w, http.StatusInternalServerError, "session lookup failed")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// requireAdmin allows only admins (403); it must run after requireUser.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := currentUser(r); u == nil || !u.IsAdmin {
			s.writeError(w, http.StatusForbidden, "admins only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects state-changing requests whose Origin is neither this
// host nor a configured public origin. SameSite=Lax cookies already stop
// most cross-site requests; this closes the rest.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && !s.originAllowed(r, origin) {
			s.writeError(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	if allowedOrigins[origin] {
		return true
	}
	for _, o := range s.PublicOrigins {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && s.TrustProxy {
		host = fh
	}
	return strings.EqualFold(u.Host, host)
}

// clientIP is the rate-limit key: the peer address, or the first proxy
// header when TrustProxy is set (behind cloudflared / Vercel / nginx).
func (s *Server) clientIP(r *http.Request) string {
	if s.TrustProxy {
		for _, h := range []string{"CF-Connecting-IP", "X-Real-IP"} {
			if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
				return v
			}
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			return strings.TrimSpace(strings.Split(v, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

// startSession creates a session for u and sets the cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User) bool {
	token, hash, err := auth.NewToken()
	if err == nil {
		err = store.CreateSession(r.Context(), s.Pool, u.ID, hash, sessionTTL)
	}
	if err != nil {
		s.Log.Error("create session failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not start a session")
		return false
	}
	s.setSessionCookie(w, token)
	return true
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return dec.Decode(v)
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	if !s.SignupEnabled {
		s.writeError(w, http.StatusForbidden, "signups are closed")
		return
	}
	if !s.signupLimit.Allow(s.clientIP(r)) {
		s.writeError(w, http.StatusTooManyRequests, "too many signups from your network; try again later")
		return
	}
	var c credentials
	if err := decodeJSON(r, &c); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	email, err := auth.NormalizeEmail(c.Email)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.CheckPasswordPolicy(c.Password); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(c.Name)
	if len(name) > 100 {
		name = name[:100]
	}
	hash, err := auth.HashPassword(c.Password)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	u, err := store.CreateUser(r.Context(), s.Pool, email, name, hash, false)
	if errors.Is(err, store.ErrEmailTaken) {
		s.writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		s.Log.Error("signup failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not create the account")
		return
	}
	s.Log.Info("user signed up", "user_id", u.ID)
	if s.startSession(w, r, u) {
		s.writeJSON(w, http.StatusCreated, map[string]any{"user": u})
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.Allow(s.clientIP(r)) {
		s.writeError(w, http.StatusTooManyRequests, "too many login attempts; try again in a few minutes")
		return
	}
	var c credentials
	if err := decodeJSON(r, &c); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	u, hash, err := store.UserCredentials(r.Context(), s.Pool, strings.TrimSpace(c.Email))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.Log.Error("login lookup failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	if u == nil || hash == "" {
		auth.EqualizeTiming(c.Password)
		s.writeError(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	if !auth.CheckPassword(hash, c.Password) {
		s.writeError(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	if s.startSession(w, r, u) {
		s.writeJSON(w, http.StatusOK, map[string]any{"user": u})
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if h := sessionHash(r); h != "" {
		if err := store.DeleteSession(r.Context(), s.Pool, h); err != nil {
			s.Log.Error("logout failed", "err", err)
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"user": currentUser(r)})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "bad JSON body")
		return
	}
	u := currentUser(r)
	hash, err := store.PasswordHash(r.Context(), s.Pool, u.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if hash == "" || !auth.CheckPassword(hash, body.Current) {
		s.writeError(w, http.StatusUnauthorized, "current password is wrong")
		return
	}
	if err := auth.CheckPasswordPolicy(body.New); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	newHash, err := auth.HashPassword(body.New)
	if err == nil {
		err = store.SetPassword(r.Context(), s.Pool, u.ID, newHash, sessionHash(r))
	}
	if err != nil {
		s.Log.Error("change password failed", "err", err)
		s.writeError(w, http.StatusInternalServerError, "could not change the password")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
