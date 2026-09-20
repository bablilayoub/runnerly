package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/auth"
	"github.com/bablilayoub/runnerly/internal/github"
)

// configIn writes a config file in a temp directory and returns its path. The
// credentials file lands beside it, so tests never touch a real home directory.
func configIn(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// userHandler answers GET /user the way GitHub does.
func userHandler(login, scopes string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if scopes != "" {
			w.Header().Set("X-OAuth-Scopes", scopes)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"` + login + `"}`))
	}
}

func TestLoginStoresVerifiedToken(t *testing.T) {
	cfg := configIn(t, "github:\n  host: github.com\n")
	opts := []option{
		withGitHub(t, userHandler("octocat", "repo, workflow")),
		withStdin("ghp_abcdefgh1234\n"),
	}

	out, _, err := runCLI(t, opts, "--config", cfg, "login", "--with-token")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	for _, want := range []string{"octocat", "not encrypted", "RUNNERLY_GITHUB_TOKEN"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ghp_abcdefgh1234") {
		t.Errorf("login echoed the token back:\n%s", out)
	}

	creds, err := auth.Load(auth.Path(cfg))
	if err != nil {
		t.Fatal(err)
	}
	host := creds.Hosts["github.com"]
	if host.Token != "ghp_abcdefgh1234" || host.User != "octocat" {
		t.Errorf("stored credential = %+v", host)
	}
}

func TestLoginRejectsATokenGitHubDoesNotAccept(t *testing.T) {
	cfg := configIn(t, "")
	opts := []option{
		withGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		}),
		withStdin("ghp_revoked\n"),
	}

	_, _, err := runCLI(t, opts, "--config", cfg, "login", "--with-token")
	if err == nil {
		t.Fatal("login stored a token GitHub rejected")
	}
	if _, statErr := os.Stat(auth.Path(cfg)); !os.IsNotExist(statErr) {
		t.Error("a rejected token must not be written to disk")
	}
}

func TestLoginWithoutWithTokenExplainsItself(t *testing.T) {
	cfg := configIn(t, "")
	_, _, err := runCLI(t, nil, "--config", cfg, "login")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "--with-token") {
		t.Errorf("error should show how to supply a token, got: %v", err)
	}
}

func TestLogoutRemovesTheStoredToken(t *testing.T) {
	cfg := configIn(t, "")
	if err := auth.Store(auth.Path(cfg), "github.com", auth.Host{Token: "t", User: "octocat"}); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCLI(t, nil, "--config", cfg, "logout", "--yes")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !strings.Contains(out, "did not revoke") {
		t.Errorf("logout should say the token is still live on GitHub:\n%s", out)
	}
	if _, statErr := os.Stat(auth.Path(cfg)); !os.IsNotExist(statErr) {
		t.Error("the credentials file should be gone")
	}
}

func TestLogoutRefusesToAssumeYesWithoutATerminal(t *testing.T) {
	cfg := configIn(t, "")
	if err := auth.Store(auth.Path(cfg), "github.com", auth.Host{Token: "t"}); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCLI(t, nil, "--config", cfg, "logout")
	if err == nil {
		t.Fatal("logout removed a token without confirmation")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should point at --yes, got: %v", err)
	}
	if _, statErr := os.Stat(auth.Path(cfg)); statErr != nil {
		t.Error("the credential was removed despite the refusal")
	}
}

func TestLogoutPromptDeclined(t *testing.T) {
	cfg := configIn(t, "")
	if err := auth.Store(auth.Path(cfg), "github.com", auth.Host{Token: "t"}); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCLI(t, []option{withInteractive(), withStdin("n\n")}, "--config", cfg, "logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !strings.Contains(out, "nothing was removed") {
		t.Errorf("output = %q", out)
	}
	if _, statErr := os.Stat(auth.Path(cfg)); statErr != nil {
		t.Error("declining the prompt still removed the credential")
	}
}

func TestAuthStatusReportsTheTokenSource(t *testing.T) {
	cfg := configIn(t, "")
	if err := auth.Store(auth.Path(cfg), "github.com", auth.Host{Token: "t", User: "octocat"}); err != nil {
		t.Fatal(err)
	}

	opts := []option{withGitHub(t, userHandler("octocat", "repo"))}
	out, _, err := runCLI(t, opts, "--config", cfg, "auth", "status")
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}
	for _, want := range []string{"octocat", "credentials.yaml", "scopes: repo"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestAuthStatusExitsNonZeroWithoutACredential(t *testing.T) {
	cfg := configIn(t, "")
	out, _, err := runCLI(t, nil, "--config", cfg, "auth", "status", "--offline")

	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 1 {
		t.Fatalf("error = %v, want ExitError{1}", err)
	}
	if !strings.Contains(out, "runnerly login") {
		t.Errorf("output should say how to authenticate:\n%s", out)
	}
}

func TestAuthStatusJSON(t *testing.T) {
	cfg := configIn(t, "")
	if err := auth.Store(auth.Path(cfg), "github.com", auth.Host{Token: "t"}); err != nil {
		t.Fatal(err)
	}

	opts := []option{withGitHub(t, userHandler("octocat", "repo, admin:org"))}
	out, _, err := runCLI(t, opts, "--config", cfg, "auth", "status", "--json")
	if err != nil {
		t.Fatalf("auth status --json: %v", err)
	}

	var got authStatus
	if jsonErr := json.Unmarshal([]byte(out), &got); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jsonErr, out)
	}
	if !got.Authenticated || got.User != "octocat" || got.Source != string(auth.SourceFile) {
		t.Errorf("status = %+v", got)
	}
}

func TestTokenFlagBeatsStoredCredential(t *testing.T) {
	cfg := configIn(t, "")
	if err := auth.Store(auth.Path(cfg), "github.com", auth.Host{Token: "stored"}); err != nil {
		t.Fatal(err)
	}

	var seen string
	opts := []option{withGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		seen = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		userHandler("octocat", "repo")(w, r)
	})}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "from-flag", "auth", "status"); err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if seen != "from-flag" {
		t.Errorf("token sent = %q, want the flag value", seen)
	}
}

func TestRepoListShowsOnlyAdministrableRepositoriesByDefault(t *testing.T) {
	cfg := configIn(t, "")
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/user/repos") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"full_name":"acme/widgets","private":true,"permissions":{"admin":true}},
			{"full_name":"acme/readonly","private":false,"permissions":{"admin":false}}
		]`))
	}

	out, _, err := runCLI(t, []option{withGitHub(t, handler)}, "--config", cfg, "--token", "t", "repo", "list")
	if err != nil {
		t.Fatalf("repo list: %v", err)
	}
	if !strings.Contains(out, "acme/widgets") {
		t.Errorf("output missing the administrable repository:\n%s", out)
	}
	if strings.Contains(out, "acme/readonly") {
		t.Errorf("output included a repository the token cannot administer:\n%s", out)
	}

	all, _, err := runCLI(t, []option{withGitHub(t, handler)}, "--config", cfg, "--token", "t", "repo", "list", "--all")
	if err != nil {
		t.Fatalf("repo list --all: %v", err)
	}
	if !strings.Contains(all, "acme/readonly") {
		t.Errorf("--all should widen the list:\n%s", all)
	}
}

func TestRepoListEmptyExplainsTheFilter(t *testing.T) {
	cfg := configIn(t, "")
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}

	out, _, err := runCLI(t, []option{withGitHub(t, handler)}, "--config", cfg, "--token", "t", "repo", "list")
	if err != nil {
		t.Fatalf("repo list: %v", err)
	}
	if !strings.Contains(out, "--all") {
		t.Errorf("an empty list should mention the admin filter:\n%s", out)
	}
}

// runnersHandler answers the runner list endpoint for one scope path.
func runnersHandler(t *testing.T, wantPath, body string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("requested %q, want %q", r.URL.Path, wantPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

const twoRunners = `{"total_count":2,"runners":[
	{"id":7,"name":"runnerly-01","os":"linux","status":"online","busy":false,
	 "labels":[{"name":"linux"},{"name":"x64"}]},
	{"id":8,"name":"runnerly-02","os":"linux","status":"online","busy":true,
	 "labels":[{"name":"linux"}]}
]}`

func TestRunnerListUsesTheRepositoryFromTheConfiguration(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	opts := []option{withGitHub(t, runnersHandler(t, "/repos/acme/widgets/actions/runners", twoRunners))}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "runner", "list")
	if err != nil {
		t.Fatalf("runner list: %v", err)
	}
	for _, want := range []string{"runnerly-01", "online", "runnerly-02", "busy", "linux,x64"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunnerListRepoFlagOverridesTheConfiguration(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	opts := []option{withGitHub(t, runnersHandler(t, "/repos/other/project/actions/runners", twoRunners))}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "list", "--repo", "other/project"); err != nil {
		t.Fatalf("runner list --repo: %v", err)
	}
}

func TestRunnerListOrgScope(t *testing.T) {
	cfg := configIn(t, "")
	opts := []option{withGitHub(t, runnersHandler(t, "/orgs/acme/actions/runners", twoRunners))}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "list", "--org", "acme"); err != nil {
		t.Fatalf("runner list --org: %v", err)
	}
}

func TestRunnerCommandsRejectBothScopeFlags(t *testing.T) {
	cfg := configIn(t, "")
	_, _, err := runCLI(t, nil, "--config", cfg, "--token", "t",
		"runner", "list", "--repo", "a/b", "--org", "c")
	if err == nil {
		t.Fatal("--repo and --org were accepted together")
	}
}

func TestRunnerListWithoutAScopeIsActionable(t *testing.T) {
	cfg := configIn(t, "")
	_, _, err := runCLI(t, nil, "--config", cfg, "--token", "t", "runner", "list")
	if err == nil {
		t.Fatal("expected an error when no scope is configured")
	}
	if !strings.Contains(err.Error(), "--repo") {
		t.Errorf("error should say how to supply a scope, got: %v", err)
	}
}

func TestRunnerListEmpty(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	opts := []option{withGitHub(t, runnersHandler(t,
		"/repos/acme/widgets/actions/runners", `{"total_count":0,"runners":[]}`))}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "runner", "list")
	if err != nil {
		t.Fatalf("runner list: %v", err)
	}
	if !strings.Contains(out, "no runners registered") {
		t.Errorf("output = %q", out)
	}
}

func TestRunnerListJSON(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	opts := []option{withGitHub(t, runnersHandler(t, "/repos/acme/widgets/actions/runners", twoRunners))}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "runner", "list", "--json")
	if err != nil {
		t.Fatalf("runner list --json: %v", err)
	}
	var runners []github.Runner
	if jsonErr := json.Unmarshal([]byte(out), &runners); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jsonErr, out)
	}
	if len(runners) != 2 || runners[0].Name != "runnerly-01" {
		t.Errorf("runners = %+v", runners)
	}
}

func TestRunnerStatus(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	opts := []option{withGitHub(t, runnersHandler(t, "/repos/acme/widgets/actions/runners", twoRunners))}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "runner", "status", "runnerly-02")
	if err != nil {
		t.Fatalf("runner status: %v", err)
	}
	for _, want := range []string{"runnerly-02", "busy", "acme/widgets"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunnerStatusUnknownNameIsActionable(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	opts := []option{withGitHub(t, runnersHandler(t, "/repos/acme/widgets/actions/runners", twoRunners))}

	_, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "runner", "status", "absent")
	if err == nil {
		t.Fatal("expected an error for an unknown runner")
	}
	if !strings.Contains(err.Error(), "runnerly runner list") {
		t.Errorf("error should suggest listing runners, got: %v", err)
	}
}

func TestRunnerRemoveByName(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	var deleted string
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(twoRunners))
	}

	out, _, err := runCLI(t, []option{withGitHub(t, handler)},
		"--config", cfg, "--token", "t", "runner", "remove", "runnerly-01", "--yes")
	if err != nil {
		t.Fatalf("runner remove: %v", err)
	}
	if deleted != "/repos/acme/widgets/actions/runners/7" {
		t.Errorf("deleted %q, want the runner resolved from its name", deleted)
	}
	if !strings.Contains(out, "stop it there too") {
		t.Errorf("remove should warn that the machine is untouched:\n%s", out)
	}
}

func TestRunnerRemoveByID(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	var deleted string
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("removing by ID should not need a lookup, but it requested %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}

	if _, _, err := runCLI(t, []option{withGitHub(t, handler)},
		"--config", cfg, "--token", "t", "runner", "remove", "--id", "42", "--yes"); err != nil {
		t.Fatalf("runner remove --id: %v", err)
	}
	if deleted != "/repos/acme/widgets/actions/runners/42" {
		t.Errorf("deleted %q", deleted)
	}
}

func TestRunnerRemoveRequiresConfirmation(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			t.Error("remove deleted a runner without confirmation")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(twoRunners))
	}

	_, _, err := runCLI(t, []option{withGitHub(t, handler)},
		"--config", cfg, "--token", "t", "runner", "remove", "runnerly-01")
	if err == nil {
		t.Fatal("expected a refusal without a terminal to confirm on")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should point at --yes, got: %v", err)
	}
}

func TestRunnerRemoveNeedsANameOrID(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	_, _, err := runCLI(t, nil, "--config", cfg, "--token", "t", "runner", "remove", "--yes")
	if err == nil || !strings.Contains(err.Error(), "--id") {
		t.Errorf("error = %v, want a complaint about the missing target", err)
	}

	_, _, err = runCLI(t, nil, "--config", cfg, "--token", "t",
		"runner", "remove", "runnerly-01", "--id", "7", "--yes")
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("error = %v, want a complaint about passing both", err)
	}
}

func TestGitHubErrorsReachTheOperator(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	handler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}

	_, _, err := runCLI(t, []option{withGitHub(t, handler)},
		"--config", cfg, "--token", "t", "runner", "list")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The hint matters more than the status code: a 404 here usually means
	// the token cannot see the repository.
	if !strings.Contains(err.Error(), "admin access") {
		t.Errorf("error should explain what a 404 means here, got: %v", err)
	}
}
