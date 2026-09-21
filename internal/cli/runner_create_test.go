package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/runner"
	"github.com/bablilayoub/runnerly/internal/state"
)

// withRunnerEnv replaces the machine-facing half of installation, so no
// archive is fetched and no script is executed.
func withRunnerEnv(record *[]string) option {
	return func(e *env) {
		e.runnerEnv = func() runner.Env {
			return runner.Env{
				GOOS:       "linux",
				GOARCH:     "amd64",
				HTTPClient: http.DefaultClient,
				Run: func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
					*record = append(*record, name+" "+strings.Join(args, " "))
					// config.sh writes .runner on success.
					if err := os.WriteFile(filepath.Join(dir, ".runner"), []byte("{}"), 0o600); err != nil {
						return nil, err
					}
					return []byte("Runner successfully added"), nil
				},
			}
		}
	}
}

// preUnpacked creates a directory that already holds the runner scripts, so
// Install has nothing to download. Extraction is covered in internal/runner.
func preUnpacked(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "runner")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.sh"), []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

// createHandler serves everything `runner create` asks GitHub for.
func createHandler(t *testing.T, private bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/runners/registration-token"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"AREGTOKEN","expires_at":"2099-01-01T00:00:00Z"}`))

		case strings.HasSuffix(r.URL.Path, "/actions/runners/downloads"):
			if err := json.NewEncoder(w).Encode([]github.Download{{
				OS: "linux", Architecture: "x64",
				Filename:       "actions-runner-linux-x64.tar.gz",
				DownloadURL:    "http://127.0.0.1:1/unused.tar.gz",
				SHA256Checksum: "deadbeef",
			}}); err != nil {
				t.Fatal(err)
			}

		case r.URL.Path == "/repos/acme/widgets":
			_, _ = w.Write([]byte(`{"full_name":"acme/widgets","name":"widgets","private":` +
				boolJSON(private) + `,"owner":{"login":"acme"},"permissions":{"admin":true}}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestRunnerCreateRegistersAPrivateRepository(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\nrunner:\n  labels: [linux, x64, docker]\n")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, true)), withRunnerEnv(&commands)}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "runnerly-01", "--dir", dir)
	if err != nil {
		t.Fatalf("runner create: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("ran %d commands, want config.sh once: %v", len(commands), commands)
	}
	for _, want := range []string{
		"--url https://github.com/acme/widgets",
		"--token AREGTOKEN",
		"--name runnerly-01",
		"--labels self-hosted,linux,x64,docker",
	} {
		if !strings.Contains(commands[0], want) {
			t.Errorf("config.sh invocation missing %q:\n%s", want, commands[0])
		}
	}

	for _, want := range []string{
		"runnerly-01 is registered",
		"acme/widgets",
		"runnerly agent run runnerly-01",
		// Whichever one this machine can actually follow: pointing a Mac
		// at `agent systemd` is advice for a service manager it does not
		// have, and this is the line people read right after setup.
		"runnerly agent " + serviceGenerator() + " runnerly-01",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "AREGTOKEN") {
		t.Errorf("the registration token leaked into the output:\n%s", out)
	}

	// The install is recorded, so later commands need no --repo.
	recorded, err := state.Get(state.Path(cfg), "runnerly-01")
	if err != nil {
		t.Fatalf("the runner was not recorded: %v", err)
	}
	if recorded.Scope.String() != "acme/widgets" {
		t.Errorf("recorded scope = %q", recorded.Scope)
	}
	if recorded.Dir != dir {
		t.Errorf("recorded dir = %q, want %q", recorded.Dir, dir)
	}
	if strings.Join(recorded.Labels, ",") != "self-hosted,linux,x64,docker" {
		t.Errorf("recorded labels = %v", recorded.Labels)
	}
}

func TestRecordedRunnerRemovesTheNeedForRepoFlag(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, true)), withRunnerEnv(&commands)}
	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "solo", "--dir", dir); err != nil {
		t.Fatalf("runner create: %v", err)
	}

	// A configuration with no repository at all: the scope must come from the
	// recorded runner.
	bare := filepath.Join(filepath.Dir(cfg), "bare.yaml")
	if err := os.WriteFile(bare, []byte("github:\n  host: github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	listed := []option{withGitHub(t, runnersHandler(t, "/repos/acme/widgets/actions/runners", twoRunners))}
	if _, _, err := runCLI(t, listed, "--config", bare, "--token", "t", "runner", "list"); err != nil {
		t.Fatalf("runner list without --repo: %v", err)
	}
}

func TestRunnerCreateRefusesAPublicRepositoryByDefault(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, false)), withRunnerEnv(&commands)}

	_, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "runnerly-01", "--dir", dir)
	if err == nil {
		t.Fatal("runner create registered against a public repository by default")
	}
	for _, want := range []string{"public repository", "allow_public_repositories", "docs/security.md", "--allow-public"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
	if len(commands) != 0 {
		t.Error("config.sh ran despite the policy refusal")
	}
}

func TestRunnerCreateAllowsAPublicRepositoryWhenAsked(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, false)), withRunnerEnv(&commands)}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "r", "--dir", dir, "--allow-public"); err != nil {
		t.Fatalf("runner create --allow-public: %v", err)
	}
	if len(commands) != 1 {
		t.Errorf("config.sh did not run: %v", commands)
	}
}

func TestRunnerCreateHonorsTheConfiguredPolicy(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\nsecurity:\n  allow_public_repositories: true\n")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, false)), withRunnerEnv(&commands)}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "r", "--dir", dir); err != nil {
		t.Fatalf("runner create: %v", err)
	}
	if len(commands) != 1 {
		t.Errorf("config.sh did not run: %v", commands)
	}
}

func TestRunnerCreateLabelsFlagOverridesConfiguration(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\nrunner:\n  labels: [linux, x64]\n")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, true)), withRunnerEnv(&commands)}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "r", "--dir", dir, "--labels", "gpu, big-memory"); err != nil {
		t.Fatalf("runner create: %v", err)
	}
	if !strings.Contains(commands[0], "--labels self-hosted,linux,x64,gpu,big-memory") {
		t.Errorf("labels = %s", commands[0])
	}
}

func TestRunnerCreateRejectsABadName(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	_, _, err := runCLI(t, nil, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "my runner")
	if err == nil || !strings.Contains(err.Error(), "not a usable runner name") {
		t.Errorf("error = %v", err)
	}
}

func TestRunnerCreateUsesTheConfiguredName(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\nrunner:\n  name: from-config\n")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, true)), withRunnerEnv(&commands)}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--dir", dir); err != nil {
		t.Fatalf("runner create: %v", err)
	}
	if !strings.Contains(commands[0], "--name from-config") {
		t.Errorf("name = %s", commands[0])
	}
}

func TestRunnerCreateRefusesToReconfigureWithoutReplace(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)
	if err := os.WriteFile(filepath.Join(dir, ".runner"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, true)), withRunnerEnv(&commands)}

	_, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--name", "r", "--dir", dir)
	if err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Errorf("error = %v, want a refusal offering --replace", err)
	}
}

func TestRunnerCreateOrganizationScopeWarnsAboutThePolicy(t *testing.T) {
	cfg := configIn(t, "")
	dir := preUnpacked(t)

	var commands []string
	opts := []option{withGitHub(t, createHandler(t, true)), withRunnerEnv(&commands)}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"runner", "create", "--org", "acme", "--name", "r", "--dir", dir)
	if err != nil {
		t.Fatalf("runner create --org: %v", err)
	}
	if !strings.Contains(out, "not checked against the public-repository policy") {
		t.Errorf("an organization runner should say the policy was not applied:\n%s", out)
	}
	if !strings.Contains(commands[0], "--url https://github.com/acme") {
		t.Errorf("url = %s", commands[0])
	}
}
