package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/store"
)

// oauthScopes is what Runnerly asks GitHub for at sign-in.
//
// Only enough to identify the person and see the organizations they belong
// to. The dashboard reads Runnerly's own database, not GitHub, so it does not
// need repository access to display anything.
const oauthScopes = "read:user read:org"

// oauthStateMaxAge bounds how long a sign-in may take. Long enough for a slow
// human, short enough that a stolen state value is useless.
const oauthStateMaxAge = 10 * 60

// callbackURL is where GitHub sends the operator back.
func (s *Server) callbackURL() string {
	if s.publicURL == "" {
		return "<server.url>/api/v1/auth/github/callback"
	}
	return s.publicURL + "/api/v1/auth/github/callback"
}

// AuthConfigResponse tells the dashboard whether signing in can work.
//
// It is public and unauthenticated on purpose: the sign-in page has to know
// before it offers a button. Without it the page would show one that leads
// to a 501, which is a worse first impression than saying so up front.
type AuthConfigResponse struct {
	SignInAvailable bool   `json:"sign_in_available"`
	Reason          string `json:"reason,omitempty"`
	Hint            string `json:"hint,omitempty"`
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	if s.oauthConfigured() && s.publicURL != "" {
		s.writeJSON(w, r, http.StatusOK, AuthConfigResponse{SignInAvailable: true})
		return
	}
	// Nothing secret here: it names settings, not their values.
	s.writeJSON(w, r, http.StatusOK, AuthConfigResponse{
		SignInAvailable: false,
		Reason:          s.oauthReason(),
		Hint:            s.oauthHint(),
	})
}

func (s *Server) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.oauthConfigured() || s.publicURL == "" {
		s.oauthUnavailable(w, r)
		return
	}

	state, err := newOAuthState()
	if err != nil {
		s.failInternal(w, r, err, "start sign-in")
		return
	}
	// The state lives in a cookie rather than the database: it is only
	// meaningful to the browser that started the flow, and comparing it on
	// the way back is what proves the callback belongs to this request.
	s.setCookie(w, oauthCookie, state, oauthStateMaxAge)

	query := url.Values{
		"client_id":    {s.oauth.ClientID},
		"redirect_uri": {s.callbackURL()},
		"scope":        {oauthScopes},
		"state":        {state},
	}
	http.Redirect(w, r, github.WebURL(s.oauth.GitHubHost)+"/login/oauth/authorize?"+query.Encode(),
		http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oauthConfigured() || s.publicURL == "" {
		s.oauthUnavailable(w, r)
		return
	}

	if reason := r.URL.Query().Get("error"); reason != "" {
		description := r.URL.Query().Get("error_description")
		if description == "" {
			description = reason
		}
		s.fail(w, r, http.StatusBadRequest, "oauth_denied",
			"GitHub refused the sign-in: "+description, "")
		return
	}

	cookie, err := r.Cookie(oauthCookie)
	state := r.URL.Query().Get("state")
	if err != nil || cookie.Value == "" || state == "" || !store.SecureCompare(cookie.Value, state) {
		// Either the flow did not start here or someone is replaying a
		// callback. Both get the same answer.
		s.fail(w, r, http.StatusBadRequest, "oauth_state_mismatch",
			"This sign-in did not start here, or it took too long.",
			"Start again at /api/v1/auth/github.")
		return
	}
	s.setCookie(w, oauthCookie, "", -1)

	code := r.URL.Query().Get("code")
	if code == "" {
		s.fail(w, r, http.StatusBadRequest, "oauth_no_code",
			"GitHub did not return an authorization code.", "Start again at /api/v1/auth/github.")
		return
	}

	accessToken, err := s.exchangeCode(r.Context(), code)
	if err != nil {
		s.log(r).Error("the OAuth code exchange failed", "event", "oauth_failed", "error", err.Error())
		s.fail(w, r, http.StatusBadGateway, "oauth_exchange_failed",
			"GitHub would not exchange the authorization code.",
			"Check that server.oauth.client_id and client_secret match the OAuth App, "+
				"and that its callback URL is "+s.callbackURL())
		return
	}

	identity, err := github.NewForHost(s.oauth.GitHubHost, accessToken,
		github.WithHTTPClient(s.httpClient)).Identify(r.Context())
	if err != nil {
		s.log(r).Error("could not identify the signed-in user",
			"event", "oauth_failed", "error", err.Error())
		s.fail(w, r, http.StatusBadGateway, "oauth_identify_failed",
			"GitHub accepted the sign-in but would not say who it was for.", "")
		return
	}

	if !s.loginAllowed(identity.User.Login) {
		s.log(r).Warn("refused a sign-in that is not on the allow list",
			"event", "oauth_refused", "user", identity.User.Login)
		s.fail(w, r, http.StatusForbidden, "login_not_allowed",
			fmt.Sprintf("%s is not allowed to sign in to this server.", identity.User.Login),
			"Add the login to server.oauth.allowed_logins.")
		return
	}

	// The user's GitHub token is stored only when there is a key to seal it
	// with. Running without one is a legitimate choice: the dashboard reads
	// Runnerly's own database, so it does not need the token afterwards.
	var sealed []byte
	if s.key != nil {
		sealed, err = s.key.EncryptString(accessToken)
		if err != nil {
			s.failInternal(w, r, err, "encrypt the user token")
			return
		}
	}

	user, err := s.store.UpsertUser(r.Context(), store.User{
		GitHubID:  identity.User.ID,
		Login:     identity.User.Login,
		Name:      identity.User.Name,
		AvatarURL: "",
	}, sealed)
	if err != nil {
		s.failInternal(w, r, err, "record the user")
		return
	}

	session, expires, err := s.store.CreateSession(r.Context(), user.ID)
	if err != nil {
		s.failInternal(w, r, err, "create the session")
		return
	}
	s.setCookie(w, sessionCookie, session, int(expires.Sub(s.now()).Seconds()))

	if err := s.store.RecordAudit(r.Context(), user.Login, "user.login", user.Login, nil); err != nil {
		s.log(r).Warn("could not record the audit entry",
			"event", "audit_write_failed", "error", err.Error())
	}
	s.log(r).Info("user signed in", "event", "user_signed_in", "user", user.Login)

	// The dashboard is not built yet, so there is nowhere to send the
	// operator. Saying so beats a redirect to a 404.
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"signed_in_as": user.Login,
		"note":         "The web dashboard is not built yet. The session cookie is set, so the API is usable.",
	})
}

// exchangeCode swaps an authorization code for an access token.
func (s *Server) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"client_id":     {s.oauth.ClientID},
		"client_secret": {s.oauth.ClientSecret},
		"code":          {code},
		"redirect_uri":  {s.callbackURL()},
	}

	endpoint := github.WebURL(s.oauth.GitHubHost) + "/login/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build the token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRequestBody))
	if err != nil {
		return "", fmt.Errorf("read the token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned HTTP %d", endpoint, resp.StatusCode)
	}

	var payload struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("the token response was not JSON: %w", err)
	}
	if payload.Error != "" {
		// GitHub reports OAuth failures with HTTP 200 and an error field.
		return "", fmt.Errorf("GitHub refused the exchange: %s", cmpFirst(payload.ErrorDescription, payload.Error))
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("GitHub returned no access token")
	}
	return payload.AccessToken, nil
}

// loginAllowed applies the allow list, if there is one.
func (s *Server) loginAllowed(login string) bool {
	if len(s.oauth.AllowedLogins) == 0 {
		return true
	}
	return slices.ContainsFunc(s.oauth.AllowedLogins, func(allowed string) bool {
		return strings.EqualFold(strings.TrimSpace(allowed), login)
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		if err := s.store.DeleteSession(r.Context(), cookie.Value); err != nil {
			s.log(r).Warn("could not delete the session",
				"event", "session_delete_failed", "error", err.Error())
		}
	}
	s.clearSessionCookie(w)
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "signed out"})
}

// SessionResponse describes who is signed in.
type SessionResponse struct {
	User store.User `json:"user"`
}

func (s *Server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	s.writeJSON(w, r, http.StatusOK, SessionResponse{User: user})
}

// cmpFirst returns the first non-empty string.
func cmpFirst(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
