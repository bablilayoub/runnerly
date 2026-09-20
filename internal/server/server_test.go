package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bablilayoub/runnerly/internal/store"
	"github.com/bablilayoub/runnerly/internal/storetest"
)

// harness is a server wired to a database schema of its own.
type harness struct {
	t      *testing.T
	server *Server
	store  *store.Store
	now    time.Time
}

func newHarness(t *testing.T, configure ...func(*Options)) *harness {
	t.Helper()

	db := storetest.New(t)
	h := &harness{t: t, store: db, now: time.Now()}

	opts := Options{
		Store: db,
		Now:   func() time.Time { return h.now },
	}
	for _, c := range configure {
		c(&opts)
	}

	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	h.server = srv
	return h
}

// do sends a request and returns the recorder.
func (h *harness) do(method, path string, body any, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatal(err)
		}
		reader = strings.NewReader(string(encoded))
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mutate {
		m(req)
	}

	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	return rec
}

// raw sends a request with a body that is not valid JSON of the right shape.
func (h *harness) raw(method, path, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, m := range mutate {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.server.ServeHTTP(rec, req)
	return rec
}

func withBearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

// signIn creates a user and a session, and returns a request mutator that
// presents its cookie.
func (h *harness) signIn(login string) func(*http.Request) {
	h.t.Helper()
	ctx := context.Background()

	user, err := h.store.UpsertUser(ctx, store.User{GitHubID: int64(len(login)) + 1000, Login: login}, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	session, _, err := h.store.CreateSession(ctx, user.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
}

// enrollmentToken issues a token an agent can enroll with.
func (h *harness) enrollmentToken(maxUses *int) string {
	h.t.Helper()
	_, secret, err := h.store.CreateEnrollmentToken(context.Background(), "test", maxUses, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	return secret
}

// enroll registers a runner and returns its machine token and row.
func (h *harness) enroll(name string) (string, store.Runner) {
	h.t.Helper()

	rec := h.do(http.MethodPost, "/api/v1/agent/register", RegisterRequest{
		Name:          name,
		GitHubHost:    "github.com",
		GitHubScope:   store.ScopeRepository,
		GitHubScopeID: "acme/widgets",
		OS:            "linux",
		Architecture:  "x64",
		Labels:        []string{"self-hosted", "linux", "x64"},
		AgentVersion:  "0.3.0",
	}, withBearer(h.enrollmentToken(nil)))

	if rec.Code != http.StatusCreated {
		h.t.Fatalf("enroll: status = %d, body = %s", rec.Code, rec.Body)
	}
	var out RegisterResponse
	decode(h.t, rec, &out)
	return out.MachineToken, out.Runner
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("response is not valid JSON: %v\n%s", err, rec.Body)
	}
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) apiError {
	t.Helper()
	var e apiError
	decode(t, rec, &e)
	return e
}

func TestHealthNeedsNoCredentials(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/v1/health", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var body HealthResponse
	decode(t, rec, &body)
	if body.Status != "ok" || body.Database != "ok" {
		t.Errorf("health = %+v", body)
	}
}

func TestUnknownEndpointExplainsItself(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/v1/nope", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if e := decodeError(t, rec); !strings.Contains(e.Hint, "/api/v1") {
		t.Errorf("error = %+v, want a hint about the API root", e)
	}
}

func TestEnrollmentRequiresAToken(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/v1/agent/register", RegisterRequest{Name: "x", GitHubScopeID: "a/b"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e := decodeError(t, rec); !strings.Contains(e.Hint, "enrollment-token") {
		t.Errorf("the hint should say how to get one: %+v", e)
	}
}

func TestEnrollmentRejectsAnUnknownToken(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/v1/agent/register", RegisterRequest{
		Name: "x", GitHubScope: store.ScopeRepository, GitHubScopeID: "a/b",
	}, withBearer("rnr_enroll_nonsense"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestEnrollmentValidatesTheBody(t *testing.T) {
	h := newHarness(t)
	token := h.enrollmentToken(nil)

	tests := []struct {
		name string
		body RegisterRequest
	}{
		{"no name", RegisterRequest{GitHubScope: store.ScopeRepository, GitHubScopeID: "a/b"}},
		{"no scope id", RegisterRequest{Name: "x", GitHubScope: store.ScopeRepository}},
		{"bad scope kind", RegisterRequest{Name: "x", GitHubScope: "enterprise", GitHubScopeID: "a/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, "/api/v1/agent/register", tt.body, withBearer(token))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
			}
		})
	}

	// None of those attempts should have spent the token.
	rec := h.do(http.MethodPost, "/api/v1/agent/register", RegisterRequest{
		Name: "valid", GitHubScope: store.ScopeRepository, GitHubScopeID: "a/b",
	}, withBearer(token))
	if rec.Code != http.StatusCreated {
		t.Errorf("a rejected body consumed the enrollment token: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestEnrollmentReturnsAWorkingMachineToken(t *testing.T) {
	h := newHarness(t)
	machineToken, runner := h.enroll("runnerly-01")

	if !strings.HasPrefix(machineToken, store.MachinePrefix) {
		t.Errorf("machine token = %q", machineToken)
	}
	if runner.Name != "runnerly-01" || runner.Status != store.StatusStarting {
		t.Errorf("runner = %+v", runner)
	}

	rec := h.do(http.MethodGet, "/api/v1/agent/config", nil, withBearer(machineToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("the machine token does not work: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestEnrollmentRespectsASingleUseToken(t *testing.T) {
	h := newHarness(t)
	once := 1
	token := h.enrollmentToken(&once)

	body := RegisterRequest{Name: "first", GitHubScope: store.ScopeRepository, GitHubScopeID: "a/b"}
	if rec := h.do(http.MethodPost, "/api/v1/agent/register", body, withBearer(token)); rec.Code != http.StatusCreated {
		t.Fatalf("first enrollment failed: %d %s", rec.Code, rec.Body)
	}

	body.Name = "second"
	rec := h.do(http.MethodPost, "/api/v1/agent/register", body, withBearer(token))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a used-up token", rec.Code)
	}
	if e := decodeError(t, rec); e.Error != "token_exhausted" {
		t.Errorf("error = %+v", e)
	}
}

func TestEnrollmentIsIdempotentForTheSameMachine(t *testing.T) {
	h := newHarness(t)
	_, first := h.enroll("runnerly-01")
	_, second := h.enroll("runnerly-01")

	if first.ID != second.ID {
		t.Errorf("re-enrolling created a new runner: %s then %s", first.ID, second.ID)
	}
}

func TestAgentEndpointsRejectMissingAndWrongCredentials(t *testing.T) {
	h := newHarness(t)
	enrollment := h.enrollmentToken(nil)

	for _, path := range []string{"/api/v1/agent/heartbeat", "/api/v1/agent/events"} {
		if rec := h.do(http.MethodPost, path, map[string]any{}); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401", path, rec.Code)
		}
		// An enrollment token is not a machine token.
		rec := h.do(http.MethodPost, path, map[string]any{}, withBearer(enrollment))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with an enrollment token = %d, want 401", path, rec.Code)
		}
	}
}

func TestHeartbeatUpdatesTheRunner(t *testing.T) {
	h := newHarness(t)
	machineToken, runner := h.enroll("runnerly-01")

	rec := h.do(http.MethodPost, "/api/v1/agent/heartbeat", HeartbeatRequest{
		Status:        store.StatusBusy,
		CPUPercent:    31.2,
		RunnerVersion: "2.337.0",
	}, withBearer(machineToken))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out HeartbeatResponse
	decode(t, rec, &out)
	if out.Runner.ID != runner.ID || out.Runner.Status != store.StatusBusy {
		t.Errorf("runner = %+v", out.Runner)
	}
	if out.Runner.LastHeartbeat == nil {
		t.Error("the heartbeat was not stamped")
	}
	if out.Config.HeartbeatIntervalSeconds <= 0 {
		t.Errorf("config = %+v, want an interval the agent can use", out.Config)
	}
}

func TestHeartbeatRejectsAnUnknownStatus(t *testing.T) {
	h := newHarness(t)
	machineToken, _ := h.enroll("runnerly-01")

	rec := h.do(http.MethodPost, "/api/v1/agent/heartbeat",
		HeartbeatRequest{Status: "on fire"}, withBearer(machineToken))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestAnAgentMayOnlyWriteEventsAboutItself(t *testing.T) {
	h := newHarness(t)
	mine, myRunner := h.enroll("runnerly-01")
	_, other := h.enroll("runnerly-02")

	rec := h.do(http.MethodPost, "/api/v1/agent/events", EventsRequest{
		Events: []AgentEvent{{Event: "runner_online", Severity: store.SeverityInfo}},
	}, withBearer(mine))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	// The event must be attributed to the authenticated runner, and the
	// other runner's feed must stay empty.
	events, err := h.store.ListEvents(context.Background(), store.ListEventsOptions{RunnerID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Event == "runner_online" {
			t.Error("an agent wrote an event onto another runner")
		}
	}

	mineEvents, err := h.store.ListEvents(context.Background(), store.ListEventsOptions{RunnerID: myRunner.ID})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range mineEvents {
		if e.Event == "runner_online" {
			found = true
		}
	}
	if !found {
		t.Error("the event was not recorded against the authenticated runner")
	}
}

func TestEventBatchIsBounded(t *testing.T) {
	h := newHarness(t)
	machineToken, _ := h.enroll("runnerly-01")

	batch := make([]AgentEvent, maxEventsPerRequest+1)
	for i := range batch {
		batch[i] = AgentEvent{Event: "noise"}
	}
	rec := h.do(http.MethodPost, "/api/v1/agent/events", EventsRequest{Events: batch}, withBearer(machineToken))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an oversized batch", rec.Code)
	}
}

func TestUnknownJSONFieldsAreRejected(t *testing.T) {
	h := newHarness(t)
	machineToken, _ := h.enroll("runnerly-01")

	rec := h.raw(http.MethodPost, "/api/v1/agent/heartbeat",
		`{"status":"online","cpu_usage":50}`, withBearer(machineToken))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: a misspelled field must not be ignored", rec.Code)
	}
}

func TestDashboardEndpointsNeedASession(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/api/v1/overview", "/api/v1/runners", "/api/v1/events", "/api/v1/enrollment-tokens",
	} {
		if rec := h.do(http.MethodGet, path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d, want 401", path, rec.Code)
		}
	}
}

func TestListRunnersReportsEffectiveStatus(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	machineToken, _ := h.enroll("runnerly-01")

	if rec := h.do(http.MethodPost, "/api/v1/agent/heartbeat",
		HeartbeatRequest{Status: store.StatusOnline}, withBearer(machineToken)); rec.Code != http.StatusOK {
		t.Fatalf("heartbeat failed: %d %s", rec.Code, rec.Body)
	}

	rec := h.do(http.MethodGet, "/api/v1/runners", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out RunnersResponse
	decode(t, rec, &out)
	if len(out.Runners) != 1 {
		t.Fatalf("got %d runners, want 1", len(out.Runners))
	}
	if out.Runners[0].Status != store.StatusOnline || out.Runners[0].Health != store.HealthOK {
		t.Errorf("runner = %+v", out.Runners[0])
	}

	// Move the clock past the offline threshold: the stored column still says
	// online, but the API must not.
	h.now = h.now.Add(10 * time.Minute)

	rec = h.do(http.MethodGet, "/api/v1/runners", nil, session)
	decode(t, rec, &out)
	if out.Runners[0].Status != store.StatusOffline {
		t.Errorf("Status = %q, want offline once heartbeats stopped", out.Runners[0].Status)
	}
	if out.Runners[0].Health != store.HealthOffline {
		t.Errorf("Health = %q, want offline", out.Runners[0].Health)
	}
	if out.Runners[0].LastHeartbeatAge == nil || *out.Runners[0].LastHeartbeatAge < 500 {
		t.Errorf("LastHeartbeatAge = %v, want the real age", out.Runners[0].LastHeartbeatAge)
	}
}

func TestGetAndDeleteRunner(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	_, runner := h.enroll("runnerly-01")

	rec := h.do(http.MethodGet, "/api/v1/runners/"+runner.ID, nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	rec = h.do(http.MethodDelete, "/api/v1/runners/"+runner.ID, nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", rec.Code, rec.Body)
	}
	// The response must be explicit that GitHub and the machine are untouched.
	if !strings.Contains(rec.Body.String(), "still registered with GitHub") {
		t.Errorf("the delete response should say what it did not do:\n%s", rec.Body)
	}

	if rec := h.do(http.MethodGet, "/api/v1/runners/"+runner.ID, nil, session); rec.Code != http.StatusNotFound {
		t.Errorf("status after delete = %d, want 404", rec.Code)
	}
}

func TestGetRunnerWithAnUnknownIDIs404(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	// A well-formed UUID that is not in the table.
	rec := h.do(http.MethodGet, "/api/v1/runners/2f1c6f5e-0000-4000-8000-000000000000", nil, session)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body = %s", rec.Code, rec.Body)
	}
}

func TestOverview(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	machineToken, _ := h.enroll("runnerly-01")

	if rec := h.do(http.MethodPost, "/api/v1/agent/heartbeat",
		HeartbeatRequest{Status: store.StatusBusy}, withBearer(machineToken)); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}

	rec := h.do(http.MethodGet, "/api/v1/overview", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out OverviewResponse
	decode(t, rec, &out)
	if out.Runners.Total != 1 || out.Runners.Busy != 1 {
		t.Errorf("counts = %+v", out.Runners)
	}
	if len(out.Events) == 0 {
		t.Error("the overview has no recent events; enrollment should have produced one")
	}
	if out.Thresholds.OfflineAfterSeconds <= out.Thresholds.StaleAfterSeconds {
		t.Errorf("thresholds = %+v", out.Thresholds)
	}
}

func TestEnrollmentTokenLifecycleOverTheAPI(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")

	rec := h.do(http.MethodPost, "/api/v1/enrollment-tokens",
		CreateEnrollmentTokenRequest{Description: "build box", ExpiresInHours: 1}, session)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var created CreateEnrollmentTokenResponse
	decode(t, rec, &created)
	if !strings.HasPrefix(created.Secret, store.EnrollmentPrefix) {
		t.Errorf("secret = %q", created.Secret)
	}
	if created.Token.ExpiresAt == nil {
		t.Error("ExpiresAt was not set")
	}

	rec = h.do(http.MethodGet, "/api/v1/enrollment-tokens", nil, session)
	var listed EnrollmentTokensResponse
	decode(t, rec, &listed)
	if len(listed.Tokens) != 1 {
		t.Fatalf("got %d tokens, want 1", len(listed.Tokens))
	}
	if strings.Contains(rec.Body.String(), created.Secret) {
		t.Error("the listing leaked the secret")
	}

	rec = h.do(http.MethodDelete, "/api/v1/enrollment-tokens/"+created.Token.ID, nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, body = %s", rec.Code, rec.Body)
	}
	// Revoking twice is a 404, not a silent success.
	if rec := h.do(http.MethodDelete, "/api/v1/enrollment-tokens/"+created.Token.ID, nil, session); rec.Code != http.StatusNotFound {
		t.Errorf("second revoke = %d, want 404", rec.Code)
	}
}

func TestCreateEnrollmentTokenValidates(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")

	zero := 0
	if rec := h.do(http.MethodPost, "/api/v1/enrollment-tokens",
		CreateEnrollmentTokenRequest{MaxUses: &zero}, session); rec.Code != http.StatusBadRequest {
		t.Errorf("max_uses 0 = %d, want 400", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/api/v1/enrollment-tokens",
		CreateEnrollmentTokenRequest{ExpiresInHours: -1}, session); rec.Code != http.StatusBadRequest {
		t.Errorf("negative expiry = %d, want 400", rec.Code)
	}
}

func TestSignInIsRefusedUntilOAuthIsConfigured(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/v1/auth/github", nil)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	e := decodeError(t, rec)
	// The operator should not have to guess which settings are missing.
	for _, want := range []string{"client_id", "client_secret", "server.url"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("message should name %q: %+v", want, e)
		}
	}
	if !strings.Contains(e.Hint, "callback") {
		t.Errorf("the hint should give the callback URL: %+v", e)
	}
}

func TestSignInRedirectsWhenConfigured(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.OAuth = OAuthConfig{ClientID: "abc", ClientSecret: "shh"}
		o.PublicURL = "https://runnerly.example.com"
	})

	rec := h.do(http.MethodGet, "/api/v1/auth/github", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302, body = %s", rec.Code, rec.Body)
	}

	location := rec.Header().Get("Location")
	for _, want := range []string{
		"github.com/login/oauth/authorize",
		"client_id=abc",
		"redirect_uri=https%3A%2F%2Frunnerly.example.com%2Fapi%2Fv1%2Fauth%2Fgithub%2Fcallback",
		"state=",
	} {
		if !strings.Contains(location, want) {
			t.Errorf("redirect missing %q:\n%s", want, location)
		}
	}
	if strings.Contains(location, "shh") {
		t.Error("the client secret is in the redirect URL")
	}

	// The state must also be set as a cookie, or the callback cannot verify it.
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == oauthCookie {
			found = true
			if !c.HttpOnly || !c.Secure {
				t.Errorf("the state cookie is not protected: %+v", c)
			}
		}
	}
	if !found {
		t.Error("no state cookie was set")
	}
}

func TestCallbackRejectsAMismatchedState(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.OAuth = OAuthConfig{ClientID: "abc", ClientSecret: "shh"}
		o.PublicURL = "https://runnerly.example.com"
	})

	rec := h.do(http.MethodGet, "/api/v1/auth/github/callback?code=x&state=forged", nil,
		func(r *http.Request) { r.AddCookie(&http.Cookie{Name: oauthCookie, Value: "real"}) })

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if e := decodeError(t, rec); e.Error != "oauth_state_mismatch" {
		t.Errorf("error = %+v", e)
	}
}

func TestCallbackReportsAGitHubRefusal(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.OAuth = OAuthConfig{ClientID: "abc", ClientSecret: "shh"}
		o.PublicURL = "https://runnerly.example.com"
	})

	rec := h.do(http.MethodGet, "/api/v1/auth/github/callback?error=access_denied&error_description=No+thanks", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if e := decodeError(t, rec); !strings.Contains(e.Message, "No thanks") {
		t.Errorf("error should carry GitHub's reason: %+v", e)
	}
}

func TestLoginAllowList(t *testing.T) {
	s := &Server{oauth: OAuthConfig{}}
	if !s.loginAllowed("anyone") {
		t.Error("an empty allow list should allow anyone")
	}

	s = &Server{oauth: OAuthConfig{AllowedLogins: []string{"octocat", " Hubot "}}}
	if !s.loginAllowed("OCTOCAT") {
		t.Error("the allow list should ignore case")
	}
	if !s.loginAllowed("hubot") {
		t.Error("the allow list should ignore surrounding spaces")
	}
	if s.loginAllowed("stranger") {
		t.Error("an account not on the list was allowed")
	}
}

func TestSessionAndLogout(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")

	rec := h.do(http.MethodGet, "/api/v1/auth/session", nil, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var out SessionResponse
	decode(t, rec, &out)
	if out.User.Login != "octocat" {
		t.Errorf("user = %+v", out.User)
	}

	if rec := h.do(http.MethodPost, "/api/v1/auth/logout", nil, session); rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d", rec.Code)
	}
	// The session must stop working straight away.
	if rec := h.do(http.MethodGet, "/api/v1/auth/session", nil, session); rec.Code != http.StatusUnauthorized {
		t.Errorf("status after logout = %d, want 401", rec.Code)
	}
}

func TestExpiredSessionIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	user, err := h.store.UpsertUser(ctx, store.User{GitHubID: 7, Login: "octocat"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := h.store.CreateSession(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Pool().Exec(ctx,
		`UPDATE sessions SET expires_at = now() - interval '1 hour'`); err != nil {
		t.Fatal(err)
	}

	rec := h.do(http.MethodGet, "/api/v1/auth/session", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if e := decodeError(t, rec); e.Error != "session_expired" {
		t.Errorf("error = %+v", e)
	}
}

func TestListEventsFilters(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	machineToken, runner := h.enroll("runnerly-01")

	if rec := h.do(http.MethodPost, "/api/v1/agent/events", EventsRequest{Events: []AgentEvent{
		{Event: "runner_online", Severity: store.SeverityInfo},
		{Event: "runner_exited", Severity: store.SeverityWarn},
	}}, withBearer(machineToken)); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}

	rec := h.do(http.MethodGet, "/api/v1/events?severity=warn", nil, session)
	var out EventsListResponse
	decode(t, rec, &out)
	if len(out.Events) != 1 || out.Events[0].Event != "runner_exited" {
		t.Errorf("events = %+v", out.Events)
	}

	rec = h.do(http.MethodGet, "/api/v1/events?runner_id="+runner.ID, nil, session)
	decode(t, rec, &out)
	if len(out.Events) < 2 {
		t.Errorf("got %d events for the runner, want at least 2", len(out.Events))
	}
}

func TestBadQueryParametersAreRejected(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")

	for _, path := range []string{"/api/v1/runners?limit=-1", "/api/v1/events?limit=abc"} {
		if rec := h.do(http.MethodGet, path, nil, session); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", path, rec.Code)
		}
	}
}

func TestNewRequiresAStore(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Error("New() accepted options with no store")
	}
}

func TestPlainHTTPCookiesAreFlaggedButStillServed(t *testing.T) {
	tests := []struct {
		name       string
		publicURL  string
		wantWarn   bool
		wantSecure bool
	}{
		{"https", "https://runnerly.example.com", false, true},
		{"loopback", "http://localhost:8080", false, false},
		{"loopback ip", "http://127.0.0.1:8080", false, false},
		{"plain http on a real host", "http://runnerly.example.com", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged strings.Builder
			logger := slog.New(slog.NewTextHandler(&logged, nil))

			h := newHarness(t, func(o *Options) {
				o.PublicURL = tt.publicURL
				o.Logger = logger
				o.OAuth = OAuthConfig{ClientID: "abc", ClientSecret: "shh"}
			})

			if got := strings.Contains(logged.String(), "insecure_cookies"); got != tt.wantWarn {
				t.Errorf("warned = %v, want %v for %s", got, tt.wantWarn, tt.publicURL)
			}
			if h.server.secureCookies != tt.wantSecure {
				t.Errorf("secureCookies = %v, want %v", h.server.secureCookies, tt.wantSecure)
			}

			// It must serve either way: refusing would break every
			// development setup and every TLS-terminating proxy.
			if rec := h.do(http.MethodGet, "/api/v1/auth/github", nil); rec.Code != http.StatusFound {
				t.Errorf("sign-in status = %d, want 302", rec.Code)
			}
		})
	}
}

func TestRestartIsQueuedForTheNextHeartbeat(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	machineToken, runner := h.enroll("runnerly-01")

	rec := h.do(http.MethodPost, "/api/v1/runners/"+runner.ID+"/restart", nil, session)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var queued RestartResponse
	decode(t, rec, &queued)
	if queued.Command.Status != store.CommandPending {
		t.Errorf("command = %+v", queued.Command)
	}
	// The response must not imply the restart already happened.
	if !strings.Contains(queued.Note, "next heartbeat") {
		t.Errorf("note should explain the delay: %q", queued.Note)
	}

	// The agent collects it on its next heartbeat.
	rec = h.do(http.MethodPost, "/api/v1/agent/heartbeat",
		HeartbeatRequest{Status: store.StatusOnline}, withBearer(machineToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, body = %s", rec.Code, rec.Body)
	}
	var beat HeartbeatResponse
	decode(t, rec, &beat)
	if len(beat.Commands) != 1 || beat.Commands[0].Command != store.CommandRestart {
		t.Fatalf("commands = %+v", beat.Commands)
	}

	// And only once. This decodes into a fresh value on purpose: `commands`
	// is omitempty, so reusing the previous one would leave the old slice in
	// place and quietly pass.
	var secondBeat HeartbeatResponse
	rec = h.do(http.MethodPost, "/api/v1/agent/heartbeat", HeartbeatRequest{}, withBearer(machineToken))
	decode(t, rec, &secondBeat)
	if len(secondBeat.Commands) != 0 {
		t.Errorf("the command was delivered twice: %+v", secondBeat.Commands)
	}

	// The agent reports the outcome.
	rec = h.do(http.MethodPost, "/api/v1/agent/commands/"+queued.Command.ID+"/result",
		CommandResultRequest{}, withBearer(machineToken))
	if rec.Code != http.StatusOK {
		t.Fatalf("result status = %d, body = %s", rec.Code, rec.Body)
	}

	rec = h.do(http.MethodGet, "/api/v1/runners/"+runner.ID+"/commands", nil, session)
	var listed CommandsResponse
	decode(t, rec, &listed)
	if len(listed.Commands) != 1 || listed.Commands[0].Status != store.CommandDone {
		t.Errorf("commands = %+v", listed.Commands)
	}
}

func TestRestartingTwiceQueuesOneCommand(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	_, runner := h.enroll("runnerly-01")

	var first, second RestartResponse
	rec := h.do(http.MethodPost, "/api/v1/runners/"+runner.ID+"/restart", nil, session)
	decode(t, rec, &first)
	rec = h.do(http.MethodPost, "/api/v1/runners/"+runner.ID+"/restart", nil, session)
	decode(t, rec, &second)

	if first.Command.ID != second.Command.ID {
		t.Error("pressing restart twice queued two restarts")
	}
}

func TestRestartAnUnknownRunnerIs404(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	rec := h.do(http.MethodPost,
		"/api/v1/runners/2f1c6f5e-0000-4000-8000-000000000000/restart", nil, session)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestRestartNeedsASession(t *testing.T) {
	h := newHarness(t)
	_, runner := h.enroll("runnerly-01")
	if rec := h.do(http.MethodPost, "/api/v1/runners/"+runner.ID+"/restart", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAnAgentCannotCompleteAnotherRunnersCommand(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("octocat")
	_, mine := h.enroll("runnerly-01")
	otherToken, _ := h.enroll("runnerly-02")

	rec := h.do(http.MethodPost, "/api/v1/runners/"+mine.ID+"/restart", nil, session)
	var queued RestartResponse
	decode(t, rec, &queued)

	rec = h.do(http.MethodPost, "/api/v1/agent/commands/"+queued.Command.ID+"/result",
		CommandResultRequest{}, withBearer(otherToken))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: one agent closed another's command", rec.Code)
	}
}

func TestAuthConfigIsPublicAndHonest(t *testing.T) {
	// Unconfigured: the sign-in page needs to know before it offers a
	// button that would lead to a 501.
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/v1/auth/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 without a session", rec.Code)
	}

	var cfg AuthConfigResponse
	decode(t, rec, &cfg)
	if cfg.SignInAvailable {
		t.Error("sign_in_available = true with no OAuth configured")
	}
	for _, want := range []string{"client_id", "client_secret"} {
		if !strings.Contains(cfg.Reason, want) {
			t.Errorf("reason should name %q: %q", want, cfg.Reason)
		}
	}
	if !strings.Contains(cfg.Hint, "callback") {
		t.Errorf("hint should give the callback URL: %q", cfg.Hint)
	}
}

func TestAuthConfigReportsAConfiguredServer(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.OAuth = OAuthConfig{ClientID: "abc", ClientSecret: "shh"}
		o.PublicURL = "https://runnerly.example.com"
	})

	rec := h.do(http.MethodGet, "/api/v1/auth/config", nil)
	var cfg AuthConfigResponse
	decode(t, rec, &cfg)

	if !cfg.SignInAvailable {
		t.Errorf("sign_in_available = false though OAuth is configured: %+v", cfg)
	}
	// It must not leak the secret while describing the configuration.
	if strings.Contains(rec.Body.String(), "shh") {
		t.Error("the client secret appears in the public auth config")
	}
}

func TestTheDashboardIsServedAndDoesNotShadowTheAPI(t *testing.T) {
	h := newHarness(t)

	// A browser route returns a page, whether or not a dashboard was built
	// into this binary.
	rec := h.do(http.MethodGet, "/runners", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /runners = %d, want 200 so the browser router can handle it", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, want HTML", rec.Header().Get("Content-Type"))
	}

	// An unknown API path must stay a JSON 404, not become a page.
	rec = h.do(http.MethodGet, "/api/v1/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Errorf("Content-Type = %q, want JSON: an API typo must not return HTML",
			rec.Header().Get("Content-Type"))
	}
}
