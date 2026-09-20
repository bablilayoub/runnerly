package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/bablilayoub/runnerly/internal/store"
)

// Cookie names.
const (
	sessionCookie = "runnerly_session"
	oauthCookie   = "runnerly_oauth_state"
)

// contextKey keeps this package's context values from colliding with anyone
// else's.
type contextKey int

const (
	runnerContextKey contextKey = iota
	userContextKey
)

func runnerFrom(ctx context.Context) (store.Runner, bool) {
	r, ok := ctx.Value(runnerContextKey).(store.Runner)
	return r, ok
}

func userFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(userContextKey).(store.User)
	return u, ok
}

// bearerToken extracts a token from the Authorization header.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// requireMachine authenticates an agent by its machine token.
func (s *Server) requireMachine(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			s.fail(w, r, http.StatusUnauthorized, "no_credentials",
				"This endpoint needs a machine token.",
				"Send it as `Authorization: Bearer rnr_machine_...`. An agent gets one by enrolling.")
			return
		}

		runner, err := s.store.AuthenticateMachine(r.Context(), token)
		if err != nil {
			if errors.Is(err, store.ErrTokenInvalid) || errors.Is(err, store.ErrNotFound) {
				s.fail(w, r, http.StatusUnauthorized, "invalid_credentials",
					"The machine token was not accepted.",
					"It may have been revoked, or its runner deleted. Enroll the machine again.")
				return
			}
			s.failInternal(w, r, err, "authenticate machine")
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), runnerContextKey, runner)))
	}
}

// requireUser authenticates a dashboard request by its session cookie.
func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			s.fail(w, r, http.StatusUnauthorized, "not_signed_in",
				"This endpoint needs a signed-in user.",
				"Sign in at /api/v1/auth/github.")
			return
		}

		user, err := s.store.AuthenticateSession(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, store.ErrTokenInvalid) {
				s.clearSessionCookie(w)
				s.fail(w, r, http.StatusUnauthorized, "session_expired",
					"The session is no longer valid.",
					"Sign in again at /api/v1/auth/github.")
				return
			}
			s.failInternal(w, r, err, "authenticate session")
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)))
	}
}

// oauthConfigured reports whether dashboard sign-in can work.
func (s *Server) oauthConfigured() bool {
	return s.oauth.ClientID != "" && s.oauth.ClientSecret != ""
}

// oauthMissing lists the settings that stop sign-in working.
func (s *Server) oauthMissing() []string {
	var missing []string
	if s.oauth.ClientID == "" {
		missing = append(missing, "server.oauth.client_id")
	}
	if s.oauth.ClientSecret == "" {
		missing = append(missing, "server.oauth.client_secret (or RUNNERLY_OAUTH_CLIENT_SECRET)")
	}
	if s.publicURL == "" {
		missing = append(missing, "server.url, which the callback URL is built from")
	}
	return missing
}

// oauthReason is the sentence shown to whoever hits a sign-in they cannot
// use. Naming the settings beats "OAuth is not configured", which sends an
// operator hunting through documentation.
func (s *Server) oauthReason() string {
	return "Dashboard sign-in is not configured: " +
		strings.Join(s.oauthMissing(), ", ") + " is not set."
}

func (s *Server) oauthHint() string {
	return "Register a GitHub OAuth App with callback " + s.callbackURL() +
		" and set those values. Runnerly ships no client credentials of its own."
}

func (s *Server) oauthUnavailable(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, http.StatusNotImplemented, "oauth_not_configured", s.oauthReason(), s.oauthHint())
}

// newOAuthState returns an unguessable value tying a callback to the request
// that started it, which is what stops an attacker completing someone else's
// sign-in.
func newOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// setCookie writes a cookie with the safe defaults every one of ours wants.
//
// Secure is conditional rather than always on, which linters flag. Setting it
// unconditionally would stop sign-in working on a plain-HTTP server with no
// visible reason, because the browser would simply never send the cookie
// back. The server instead binds to loopback by default and warns at startup
// when it is configured to serve session cookies over plain HTTP to a
// non-loopback address.
func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is set from the scheme; see above
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		// Lax rather than Strict: the OAuth callback is a top-level
		// navigation from GitHub, and Strict would drop the cookie on it.
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	s.setCookie(w, sessionCookie, "", -1)
}
