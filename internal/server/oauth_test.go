package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeGitHub stands in for the two endpoints sign-in touches: the code
// exchange and /user. It is served over TLS because Runnerly builds https
// URLs from a host name and will not be talked out of it, and httptest's
// own client is the one that trusts it.
//
// It is strict about the client id, the secret and the code, so a mistake
// on Runnerly's side comes back as a refusal rather than being waved
// through by a stub that accepts anything.
type fakeGitHub struct {
	server *httptest.Server

	// exchanged and identified record that each leg was actually used.
	exchanged, identified int
	lastRedirectURI       string
}

const (
	fakeClientID     = "Iv1.testclient"
	fakeClientSecret = "test-client-secret"
	fakeCode         = "the-authorization-code"
	fakeToken        = "gho_test_token"
	fakeLogin        = "octocat"
	fakeAvatar       = "https://avatars.githubusercontent.com/u/583231?v=4"
)

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("exchange body: %v", err)
		}
		f.exchanged++
		f.lastRedirectURI = r.PostForm.Get("redirect_uri")

		if r.PostForm.Get("client_id") != fakeClientID ||
			r.PostForm.Get("client_secret") != fakeClientSecret ||
			r.PostForm.Get("code") != fakeCode {
			writeFakeJSON(t, w, map[string]string{"error": "incorrect_client_credentials"})
			return
		}
		// GitHub answers form-encoded unless asked for JSON.
		if !strings.Contains(r.Header.Get("Accept"), "json") {
			t.Errorf("the exchange did not ask for JSON: %q", r.Header.Get("Accept"))
		}
		writeFakeJSON(t, w, map[string]string{
			"access_token": fakeToken, "token_type": "bearer", "scope": "read:user,read:org",
		})
	})
	mux.HandleFunc("GET /api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		f.identified++
		if r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			writeFakeJSON(t, w, map[string]string{"message": "Bad credentials"})
			return
		}
		w.Header().Set("X-OAuth-Scopes", "read:user, read:org")
		writeFakeJSON(t, w, map[string]any{
			"login": fakeLogin, "id": 583231, "name": "The Octocat",
			"type": "User", "avatar_url": fakeAvatar,
		})
	})

	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// host is what goes in server.oauth's GitHubHost: a bare host:port, which
// is the shape Runnerly turns into https://host/api/v3.
func (f *fakeGitHub) host() string {
	return strings.TrimPrefix(f.server.URL, "https://")
}

func writeFakeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}

// signInHarness wires a server whose GitHub is the fake one.
func signInHarness(t *testing.T, configure ...func(*Options)) (*harness, *fakeGitHub) {
	t.Helper()
	gh := newFakeGitHub(t)

	h := newHarness(t, append([]func(*Options){func(o *Options) {
		o.OAuth = OAuthConfig{
			ClientID:     fakeClientID,
			ClientSecret: fakeClientSecret,
			GitHubHost:   gh.host(),
		}
		o.PublicURL = "http://runnerly.test"
		o.HTTPClient = gh.server.Client()
	}}, configure...)...)
	return h, gh
}

// startSignIn performs the first leg and returns the state cookie.
func startSignIn(t *testing.T, h *harness) *http.Cookie {
	t.Helper()

	rec := h.do(http.MethodGet, "/api/v1/auth/github", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("start: status = %d, want 302", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookie && c.Value != "" {
			return c
		}
	}
	t.Fatal("the first leg set no state cookie, so the callback can never verify one")
	return nil
}

// callback performs the second leg with whatever code and state are given.
func callback(t *testing.T, h *harness, code, state string, jar *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	query := url.Values{}
	if code != "" {
		query.Set("code", code)
	}
	if state != "" {
		query.Set("state", state)
	}
	return h.do(http.MethodGet, "/api/v1/auth/github/callback?"+query.Encode(), nil,
		func(r *http.Request) {
			if jar != nil {
				r.AddCookie(jar)
			}
		})
}

// TestSignInCompletesAndLandsOnTheDashboard drives the whole handshake.
//
// Everything up to here had a test — the refusal when OAuth is not
// configured, and the redirect out to GitHub — and the half that actually
// signs somebody in had none. It is the half that reaches the database.
func TestSignInCompletesAndLandsOnTheDashboard(t *testing.T) {
	h, gh := signInHarness(t)
	state := startSignIn(t, h)

	rec := callback(t, h, fakeCode, state.Value, state)

	// A browser is doing this, so the end of it has to be somewhere a
	// browser can be. Returning JSON leaves the operator looking at a
	// blob after a successful sign-in.
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want a redirect; body = %s", rec.Code, rec.Body)
	}
	if location := rec.Header().Get("Location"); location != "/" {
		t.Errorf("redirected to %q, want the dashboard at /", location)
	}

	if gh.exchanged != 1 || gh.identified != 1 {
		t.Errorf("exchanged %d times, identified %d times; want 1 and 1", gh.exchanged, gh.identified)
	}
	// The redirect_uri on the exchange has to match the one sent out, or
	// GitHub refuses it — and that is a failure nothing else detects.
	if want := "http://runnerly.test/api/v1/auth/github/callback"; gh.lastRedirectURI != want {
		t.Errorf("exchange redirect_uri = %q, want %q", gh.lastRedirectURI, want)
	}

	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			session = c
		}
		if c.Name == oauthCookie && c.MaxAge >= 0 {
			t.Error("the state cookie outlived the flow it was for")
		}
	}
	if session == nil {
		t.Fatal("no session cookie, so the sign-in achieved nothing")
	}

	// And the session has to actually work.
	me := h.do(http.MethodGet, "/api/v1/auth/session", nil, func(r *http.Request) { r.AddCookie(session) })
	if me.Code != http.StatusOK {
		t.Fatalf("/api/v1/auth/session with the new session = %d, body = %s", me.Code, me.Body)
	}
}

// TestSignInStoresTheAvatar is the same class of bug as the runner version
// and the memory and disk totals: a field the dashboard renders, the
// database holds, and nothing ever filled in.
func TestSignInStoresTheAvatar(t *testing.T) {
	h, _ := signInHarness(t)
	state := startSignIn(t, h)

	rec := callback(t, h, fakeCode, state.Value, state)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body)
	}

	var session string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c.Value
		}
	}
	user, err := h.store.AuthenticateSession(context.Background(), session)
	if err != nil {
		t.Fatalf("looking up the signed-in user: %v", err)
	}
	if user.AvatarURL != fakeAvatar {
		t.Errorf("stored avatar = %q, want %q — the dashboard shows initials without it",
			user.AvatarURL, fakeAvatar)
	}
	if user.Login != fakeLogin || user.GitHubID != 583231 {
		t.Errorf("stored user = %+v", user)
	}
}

func TestSignInRefusesAReplayedCallback(t *testing.T) {
	h, gh := signInHarness(t)
	state := startSignIn(t, h)

	// No cookie: the flow did not start in this browser.
	if rec := callback(t, h, fakeCode, state.Value, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("callback with no state cookie = %d, want 400", rec.Code)
	}
	// A cookie that does not match the state in the URL.
	other := &http.Cookie{Name: oauthCookie, Value: "a-different-state"}
	if rec := callback(t, h, fakeCode, state.Value, other); rec.Code != http.StatusBadRequest {
		t.Errorf("callback with a mismatched state = %d, want 400", rec.Code)
	}
	if gh.exchanged != 0 {
		t.Error("a callback that failed its state check still went to GitHub")
	}
}

func TestSignInRefusesALoginOffTheAllowList(t *testing.T) {
	h, _ := signInHarness(t, func(o *Options) {
		o.OAuth.AllowedLogins = []string{"someone-else"}
	})
	state := startSignIn(t, h)

	rec := callback(t, h, fakeCode, state.Value, state)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			t.Error("a refused sign-in still handed out a session")
		}
	}
}

func TestSignInReportsWhatGitHubRefused(t *testing.T) {
	h, gh := signInHarness(t)
	state := startSignIn(t, h)

	rec := h.do(http.MethodGet,
		"/api/v1/auth/github/callback?error=access_denied&error_description=The+user+denied+the+request",
		nil, func(r *http.Request) { r.AddCookie(state) })

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if e := decodeError(t, rec); !strings.Contains(e.Message, "denied") {
		t.Errorf("the message does not say what GitHub said: %+v", e)
	}
	if gh.exchanged != 0 {
		t.Error("a denied sign-in still tried to exchange a code")
	}
}

func TestSignInSurvivesGitHubRejectingTheCode(t *testing.T) {
	h, _ := signInHarness(t)
	state := startSignIn(t, h)

	rec := callback(t, h, "not-the-code-github-issued", state.Value, state)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", rec.Code, rec.Body)
	}
	// The hint has to name the two settings that are usually wrong.
	e := decodeError(t, rec)
	for _, want := range []string{"client_id", "callback"} {
		if !strings.Contains(e.Hint, want) {
			t.Errorf("the hint should mention %q: %+v", want, e)
		}
	}
}
