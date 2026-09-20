package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient returns a client pointed at a test server.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("test-token", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestAPIBaseURL(t *testing.T) {
	tests := map[string]string{
		"":                         "https://api.github.com",
		"github.com":               "https://api.github.com",
		"www.github.com":           "https://api.github.com",
		"https://github.com":       "https://api.github.com",
		"github.acme.com":          "https://github.acme.com/api/v3",
		"https://github.acme.com/": "https://github.acme.com/api/v3",
	}
	for host, want := range tests {
		if got := APIBaseURL(host); got != want {
			t.Errorf("APIBaseURL(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestWebURL(t *testing.T) {
	tests := map[string]string{
		"":                "https://github.com",
		"github.com":      "https://github.com",
		"github.acme.com": "https://github.acme.com",
	}
	for host, want := range tests {
		if got := WebURL(host); got != want {
			t.Errorf("WebURL(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestParseRepository(t *testing.T) {
	want := Scope{Kind: KindRepository, Owner: "bablilayoub", Repo: "example"}
	accepted := []string{
		"bablilayoub/example",
		"  bablilayoub/example  ",
		"bablilayoub/example.git",
		"https://github.com/bablilayoub/example",
		"https://github.com/bablilayoub/example.git",
		"git@github.com:bablilayoub/example.git",
		"/bablilayoub/example/",
	}
	for _, in := range accepted {
		got, err := ParseRepository(in)
		if err != nil {
			t.Errorf("ParseRepository(%q) error = %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseRepository(%q) = %+v, want %+v", in, got, want)
		}
	}

	rejected := []string{"", "example", "owner/", "/repo", "owner/repo/extra", "own er/repo"}
	for _, in := range rejected {
		if _, err := ParseRepository(in); err == nil {
			t.Errorf("ParseRepository(%q) accepted an invalid repository", in)
		}
	}
}

func TestParseRepositoryErrorIsActionable(t *testing.T) {
	_, err := ParseRepository("example")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "owner/repo") {
		t.Errorf("error should show the expected form, got: %v", err)
	}
}

func TestScope(t *testing.T) {
	repo := Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"}
	if repo.String() != "acme/widgets" {
		t.Errorf("String() = %q", repo.String())
	}
	if repo.apiPath() != "repos/acme/widgets" {
		t.Errorf("apiPath() = %q", repo.apiPath())
	}
	if got := repo.WebURL("github.com"); got != "https://github.com/acme/widgets" {
		t.Errorf("WebURL() = %q", got)
	}

	org := ForOrganization("acme")
	if org.String() != "acme" {
		t.Errorf("String() = %q", org.String())
	}
	if org.apiPath() != "orgs/acme" {
		t.Errorf("apiPath() = %q", org.apiPath())
	}
}

func TestScopeValidate(t *testing.T) {
	tests := []struct {
		name  string
		scope Scope
		ok    bool
	}{
		{"repository", Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"}, true},
		{"organization", Scope{Kind: KindOrganization, Owner: "acme"}, true},
		{"bad owner", Scope{Kind: KindRepository, Owner: "ac me", Repo: "widgets"}, false},
		{"bad repo", Scope{Kind: KindRepository, Owner: "acme", Repo: "wid gets"}, false},
		{"org with repo", Scope{Kind: KindOrganization, Owner: "acme", Repo: "widgets"}, false},
		{"unknown kind", Scope{Kind: "enterprise", Owner: "acme"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.scope.Validate()
			if tt.ok && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
			if !tt.ok && err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

func TestRequestHeaders(t *testing.T) {
	var got http.Header
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		writeJSON(t, w, http.StatusOK, User{Login: "octocat"})
	})

	if _, err := c.Viewer(context.Background()); err != nil {
		t.Fatalf("Viewer() error = %v", err)
	}
	checks := map[string]string{
		"Authorization":        "Bearer test-token",
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": apiVersion,
		"User-Agent":           DefaultUserAgent,
	}
	for header, want := range checks {
		if got.Get(header) != want {
			t.Errorf("%s = %q, want %q", header, got.Get(header), want)
		}
	}
}

func TestUnauthenticatedClientSendsNoAuthorization(t *testing.T) {
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		writeJSON(t, w, http.StatusOK, User{Login: "octocat"})
	}))
	defer srv.Close()

	c := New("", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	if _, err := c.Viewer(context.Background()); err != nil {
		t.Fatalf("Viewer() error = %v", err)
	}
	if authorization != "" {
		t.Errorf("Authorization = %q, want it absent", authorization)
	}
}

func TestListRunnersPaginates(t *testing.T) {
	var paths []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		page := r.URL.Query().Get("page")

		body := struct {
			TotalCount int      `json:"total_count"`
			Runners    []Runner `json:"runners"`
		}{TotalCount: 3}

		switch page {
		case "1":
			body.Runners = []Runner{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}
		case "2":
			body.Runners = []Runner{{ID: 3, Name: "c"}}
		}
		writeJSON(t, w, http.StatusOK, body)
	})

	scope := Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"}
	runners, err := c.ListRunners(context.Background(), scope)
	if err != nil {
		t.Fatalf("ListRunners() error = %v", err)
	}
	if len(runners) != 3 {
		t.Fatalf("got %d runners, want 3", len(runners))
	}
	if len(paths) != 2 {
		t.Errorf("made %d requests, want 2: %v", len(paths), paths)
	}
	if !strings.HasPrefix(paths[0], "/repos/acme/widgets/actions/runners?") {
		t.Errorf("unexpected path %q", paths[0])
	}
}

func TestListRunnersStopsOnEmptyPage(t *testing.T) {
	requests := 0
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		// A server that reports more than it returns must not loop forever.
		writeJSON(t, w, http.StatusOK, map[string]any{"total_count": 99, "runners": []Runner{}})
	})

	runners, err := c.ListRunners(context.Background(), Scope{Kind: KindRepository, Owner: "a", Repo: "b"})
	if err != nil {
		t.Fatalf("ListRunners() error = %v", err)
	}
	if len(runners) != 0 {
		t.Errorf("got %d runners, want 0", len(runners))
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1", requests)
	}
}

func TestFindRunnerByName(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"total_count": 2,
			"runners": []Runner{
				{ID: 7, Name: "runnerly-01"},
				{ID: 8, Name: "runnerly-02"},
			},
		})
	})
	scope := Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"}

	got, err := c.FindRunnerByName(context.Background(), scope, "RUNNERLY-02")
	if err != nil {
		t.Fatalf("FindRunnerByName() error = %v", err)
	}
	if got.ID != 8 {
		t.Errorf("ID = %d, want 8 (matching must ignore case)", got.ID)
	}

	_, err = c.FindRunnerByName(context.Background(), scope, "absent")
	if !errors.Is(err, ErrRunnerNotFound) {
		t.Errorf("error = %v, want ErrRunnerNotFound", err)
	}
}

func TestDeleteRunner(t *testing.T) {
	var method, path string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	scope := Scope{Kind: KindOrganization, Owner: "acme"}
	if err := c.DeleteRunner(context.Background(), scope, 42); err != nil {
		t.Fatalf("DeleteRunner() error = %v", err)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", method)
	}
	if path != "/orgs/acme/actions/runners/42" {
		t.Errorf("path = %q", path)
	}
}

func TestCreateRegistrationToken(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	var gotPath, gotMethod string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		writeJSON(t, w, http.StatusCreated, RegistrationToken{Token: "ABC123", ExpiresAt: expires})
	})

	scope := Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"}
	token, err := c.CreateRegistrationToken(context.Background(), scope)
	if err != nil {
		t.Fatalf("CreateRegistrationToken() error = %v", err)
	}
	if token.Token != "ABC123" {
		t.Errorf("Token = %q", token.Token)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/repos/acme/widgets/actions/runners/registration-token" {
		t.Errorf("path = %q", gotPath)
	}
	if token.Expired(time.Now()) {
		t.Error("a token expiring in an hour reported itself expired")
	}
	if !token.Expired(expires.Add(time.Second)) {
		t.Error("a token past its expiry reported itself valid")
	}
}

func TestCreateRemoveTokenUsesItsOwnEndpoint(t *testing.T) {
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, http.StatusCreated, RegistrationToken{Token: "X"})
	})
	scope := Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"}
	if _, err := c.CreateRemoveToken(context.Background(), scope); err != nil {
		t.Fatalf("CreateRemoveToken() error = %v", err)
	}
	if gotPath != "/repos/acme/widgets/actions/runners/remove-token" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestCreateRegistrationTokenRejectsEmptyToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusCreated, map[string]any{"token": ""})
	})
	_, err := c.CreateRegistrationToken(context.Background(), Scope{Kind: KindRepository, Owner: "a", Repo: "b"})
	if err == nil || !strings.Contains(err.Error(), "empty token") {
		t.Errorf("error = %v, want a complaint about an empty token", err)
	}
}

func TestFindDownload(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, []Download{
			{OS: "linux", Architecture: "x64", Filename: "actions-runner-linux-x64.tar.gz", SHA256Checksum: "aaa"},
			{OS: "linux", Architecture: "arm64", Filename: "actions-runner-linux-arm64.tar.gz", SHA256Checksum: "bbb"},
		})
	})
	scope := Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"}

	got, err := c.FindDownload(context.Background(), scope, "linux", "arm64")
	if err != nil {
		t.Fatalf("FindDownload() error = %v", err)
	}
	if got.SHA256Checksum != "bbb" {
		t.Errorf("checksum = %q, want bbb", got.SHA256Checksum)
	}

	_, err = c.FindDownload(context.Background(), scope, "plan9", "riscv64")
	if err == nil {
		t.Fatal("FindDownload() accepted an unsupported platform")
	}
	if !strings.Contains(err.Error(), "linux/x64") {
		t.Errorf("error should list what is available, got: %v", err)
	}
}

func TestRepository(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widgets" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"id": 1, "name": "widgets", "full_name": "acme/widgets", "private": true,
			"owner":       map[string]any{"login": "acme", "type": "Organization"},
			"permissions": map[string]any{"admin": true},
		})
	})

	repo, err := c.Repository(context.Background(), Scope{Kind: KindRepository, Owner: "acme", Repo: "widgets"})
	if err != nil {
		t.Fatalf("Repository() error = %v", err)
	}
	if !repo.Private || !repo.Permissions.Admin {
		t.Errorf("repository = %+v", repo)
	}
	if got := repo.Scope(); got.String() != "acme/widgets" {
		t.Errorf("Scope() = %q", got)
	}
}

func TestRepositoryRejectsOrganizationScope(t *testing.T) {
	c := New("t")
	if _, err := c.Repository(context.Background(), ForOrganization("acme")); err == nil {
		t.Error("Repository() accepted an organization scope")
	}
}

func TestListRepositoriesFiltersAndLimits(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sort") != "pushed" {
			t.Errorf("sort = %q, want pushed", r.URL.Query().Get("sort"))
		}
		writeJSON(t, w, http.StatusOK, []map[string]any{
			{"full_name": "acme/one", "permissions": map[string]any{"admin": true}},
			{"full_name": "acme/two", "permissions": map[string]any{"admin": false}},
			{"full_name": "acme/three", "permissions": map[string]any{"admin": true}},
		})
	})

	all, err := c.ListRepositories(context.Background(), ListRepositoriesOptions{})
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d repositories, want 3", len(all))
	}

	admin, err := c.ListRepositories(context.Background(), ListRepositoriesOptions{AdminOnly: true})
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(admin) != 2 {
		t.Errorf("AdminOnly returned %d repositories, want 2", len(admin))
	}

	limited, err := c.ListRepositories(context.Background(), ListRepositoriesOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("Limit returned %d repositories, want 1", len(limited))
	}
}

func TestSearchRepositoriesFiltersByName(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, []map[string]any{
			{"full_name": "acme/widgets"},
			{"full_name": "acme/gadgets"},
		})
	})
	got, err := c.SearchRepositories(context.Background(), "WIDG", ListRepositoriesOptions{})
	if err != nil {
		t.Fatalf("SearchRepositories() error = %v", err)
	}
	if len(got) != 1 || got[0].FullName != "acme/widgets" {
		t.Errorf("got %+v, want just acme/widgets", got)
	}
}

func TestRunnerStateAndLabels(t *testing.T) {
	tests := []struct {
		runner Runner
		want   string
	}{
		{Runner{Status: "online"}, "online"},
		{Runner{Status: "online", Busy: true}, "busy"},
		{Runner{Status: "offline"}, "offline"},
		{Runner{Status: "offline", Busy: true}, "offline"},
	}
	for _, tt := range tests {
		if got := tt.runner.State(); got != tt.want {
			t.Errorf("State() = %q, want %q for %+v", got, tt.want, tt.runner)
		}
	}

	r := Runner{Labels: []Label{{Name: "linux"}, {Name: "x64"}}}
	if strings.Join(r.LabelNames(), ",") != "linux,x64" {
		t.Errorf("LabelNames() = %v", r.LabelNames())
	}
}

func TestErrorHints(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    map[string]any
		token   string
		want    []string
	}{
		{
			name:   "unauthorized with a token",
			status: http.StatusUnauthorized,
			body:   map[string]any{"message": "Bad credentials"},
			token:  "t",
			want:   []string{"invalid, expired", "runnerly login"},
		},
		{
			name:   "unauthorized without a token",
			status: http.StatusUnauthorized,
			body:   map[string]any{"message": "Requires authentication"},
			want:   []string{"No GitHub token", "RUNNERLY_GITHUB_TOKEN"},
		},
		{
			name:   "forbidden for a missing scope",
			status: http.StatusForbidden,
			headers: map[string]string{
				"X-OAuth-Scopes":          "read:user",
				"X-Accepted-OAuth-Scopes": "repo",
				"X-RateLimit-Limit":       "5000",
				"X-RateLimit-Remaining":   "4999",
			},
			body:  map[string]any{"message": "Resource not accessible"},
			token: "t",
			want:  []string{"needs one of these scopes: repo", "read:user"},
		},
		{
			name:   "rate limited",
			status: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Limit":     "60",
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     "1700000000",
			},
			body:  map[string]any{"message": "API rate limit exceeded"},
			token: "t",
			want:  []string{"rate limit resets"},
		},
		{
			name:   "not found with a token",
			status: http.StatusNotFound,
			body:   map[string]any{"message": "Not Found"},
			token:  "t",
			want:   []string{"cannot see it", "admin access"},
		},
		{
			name:   "server error",
			status: http.StatusBadGateway,
			body:   map[string]any{"message": "Server Error"},
			token:  "t",
			want:   []string{"githubstatus.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				writeJSON(t, w, tt.status, tt.body)
			}))
			defer srv.Close()

			c := New(tt.token, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
			_, err := c.Viewer(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}

			var ghErr *Error
			if !errors.As(err, &ghErr) {
				t.Fatalf("error is %T, want *github.Error", err)
			}
			if ghErr.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", ghErr.StatusCode, tt.status)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error missing %q:\n%s", want, err)
				}
			}
		})
	}
}

func TestErrorPredicates(t *testing.T) {
	cases := []struct {
		status    int
		notFound  bool
		unauth    bool
		forbidden bool
	}{
		{http.StatusNotFound, true, false, false},
		{http.StatusUnauthorized, false, true, false},
		{http.StatusForbidden, false, false, true},
	}
	for _, tt := range cases {
		err := error(&Error{StatusCode: tt.status})
		wrapped := fmt.Errorf("context: %w", err)
		if IsNotFound(wrapped) != tt.notFound {
			t.Errorf("IsNotFound(%d) = %v", tt.status, IsNotFound(wrapped))
		}
		if IsUnauthorized(wrapped) != tt.unauth {
			t.Errorf("IsUnauthorized(%d) = %v", tt.status, IsUnauthorized(wrapped))
		}
		if IsForbidden(wrapped) != tt.forbidden {
			t.Errorf("IsForbidden(%d) = %v", tt.status, IsForbidden(wrapped))
		}
	}

	limited := error(&Error{StatusCode: http.StatusForbidden, RateLimit: RateLimit{Limit: 60, Remaining: 0}})
	if !IsRateLimited(limited) {
		t.Error("IsRateLimited() = false for an exhausted limit")
	}
	scopeIssue := error(&Error{StatusCode: http.StatusForbidden, RateLimit: RateLimit{Limit: 5000, Remaining: 4999}})
	if IsRateLimited(scopeIssue) {
		t.Error("IsRateLimited() = true for a permissions failure")
	}
}

func TestIdentify(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, workflow ,admin:org")
		writeJSON(t, w, http.StatusOK, User{Login: "octocat"})
	})
	id, err := c.Identify(context.Background())
	if err != nil {
		t.Fatalf("Identify() error = %v", err)
	}
	if id.User.Login != "octocat" {
		t.Errorf("Login = %q", id.User.Login)
	}
	if strings.Join(id.Scopes, "|") != "repo|workflow|admin:org" {
		t.Errorf("scopes = %v", id.Scopes)
	}
	if id.FineGrained {
		t.Error("FineGrained = true for a token that reported scopes")
	}
}

func TestIdentifyMarksFineGrainedTokens(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		// Fine-grained tokens send no X-OAuth-Scopes header at all.
		writeJSON(t, w, http.StatusOK, User{Login: "octocat"})
	})
	id, err := c.Identify(context.Background())
	if err != nil {
		t.Fatalf("Identify() error = %v", err)
	}
	if !id.FineGrained {
		t.Error("FineGrained = false for a token that reported no scopes")
	}
}

func TestMissingScope(t *testing.T) {
	tests := []struct {
		name string
		kind ScopeKind
		have []string
		want string
	}{
		{"classic token with repo", KindRepository, []string{"repo", "workflow"}, ""},
		{"classic token without repo", KindRepository, []string{"read:user"}, "repo"},
		{"org needs admin:org", KindOrganization, []string{"repo"}, "admin:org"},
		{"org with admin:org", KindOrganization, []string{"admin:org"}, ""},
		{"fine-grained token is never flagged", KindRepository, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MissingScope(tt.kind, tt.have); got != tt.want {
				t.Errorf("MissingScope() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContextCancellationIsReported(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, User{})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Viewer(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
