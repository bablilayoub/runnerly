package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/state"
)

// setupHandler serves what setup asks GitHub for: who the token belongs to,
// whether the repository is private, a registration token, and the download
// listing.
func setupHandler(t *testing.T, private bool) http.HandlerFunc {
	t.Helper()
	create := createHandler(t, private)
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-OAuth-Scopes", "repo, workflow")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
		case "/user/repos":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"full_name":"acme/widgets","name":"widgets","private":true,
				 "owner":{"login":"acme"},"permissions":{"admin":true}},
				{"full_name":"acme/docs","name":"docs","private":false,
				 "owner":{"login":"acme"},"permissions":{"admin":true}}
			]`))
		default:
			create(w, r)
		}
	}
}

// setupOptions is the common wiring: a fake GitHub and a runner install that
// touches nothing.
func setupOptions(t *testing.T, private bool, commands *[]string, extra ...option) []option {
	t.Helper()
	return append([]option{
		withGitHub(t, setupHandler(t, private)),
		withRunnerEnv(commands),
	}, extra...)
}

func TestSetupRegistersARunnerEndToEnd(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	out, _, err := runCLI(t, setupOptions(t, true, &commands),
		"--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--name", "runnerly-01",
		"--dir", preUnpacked(t), "--yes", "--skip-doctor")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}

	if len(commands) != 1 {
		t.Fatalf("ran %d commands, want config.sh once: %v", len(commands), commands)
	}
	if !strings.Contains(commands[0], "--name runnerly-01") {
		t.Errorf("config.sh did not get the name:\n%s", commands[0])
	}

	// setup writes a configuration when there is none, so the machine is left
	// in the state the rest of the commands expect.
	if _, err := os.Stat(cfg); err != nil {
		t.Errorf("setup did not write a configuration: %v", err)
	}
	// And records the install, so no later command needs --repo.
	recorded, err := state.Get(state.Path(cfg), "runnerly-01")
	if err != nil {
		t.Fatalf("the runner was not recorded: %v", err)
	}
	if recorded.Scope.String() != "acme/widgets" {
		t.Errorf("recorded scope = %q", recorded.Scope)
	}

	for _, want := range []string{
		"signed in to github.com as octocat",
		"runnerly-01 is registered",
		"runnerly agent run runnerly-01",
		"runnerly agent systemd runnerly-01",
		"runs-on:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "AREGTOKEN") {
		t.Errorf("the registration token leaked into the output:\n%s", out)
	}
}

// Declining the plan has to leave the machine exactly as it was. A wizard
// that half-applies is worse than one that does nothing.
func TestSetupChangesNothingWhenDeclined(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := setupOptions(t, true, &commands, withInteractive(), withStdin("n\n"))
	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--skip-doctor")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}

	if len(commands) != 0 {
		t.Errorf("declining still ran %v", commands)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("declining still wrote a configuration")
	}
	if _, err := os.Stat(state.Path(cfg)); err == nil {
		t.Error("declining still recorded a runner")
	}
	if !strings.Contains(out, "Nothing was changed") {
		t.Errorf("output does not say nothing happened:\n%s", out)
	}
}

// The plan has to name every change before any of them happens, or the
// confirmation is not informed consent.
func TestSetupPlanNamesEveryChangeBeforeMakingIt(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := setupOptions(t, true, &commands, withInteractive(), withStdin("n\n"))
	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--name", "runnerly-01", "--skip-doctor")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	plan := out[strings.Index(out, "What this will do"):]
	for _, want := range []string{
		cfg,            // the configuration it would write
		"runnerly-01",  // the runner it would register
		"acme/widgets", // where
		"self-hosted",  // the labels it would use
		"Nothing is started",
	} {
		if !strings.Contains(plan, want) {
			t.Errorf("the plan does not mention %q:\n%s", want, plan)
		}
	}
}

// --yes is for provisioning scripts, so it must fail rather than block on a
// question no one is there to answer.
func TestSetupWithYesRefusesToPromptForAScope(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	_, _, err := runCLI(t, setupOptions(t, true, &commands),
		"--config", cfg, "--token", "t", "setup", "--yes", "--skip-doctor")
	if err == nil {
		t.Fatal("setup --yes with no repository succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "--repo") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
	if len(commands) != 0 {
		t.Errorf("it registered something anyway: %v", commands)
	}
}

// The public-repository policy is the one guard that must not be softer in
// the wizard than in `runner create`.
func TestSetupEnforcesThePublicRepositoryPolicy(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	_, _, err := runCLI(t, setupOptions(t, false, &commands),
		"--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--yes", "--skip-doctor")
	if err == nil {
		t.Fatal("setup registered against a public repository")
	}
	if !strings.Contains(err.Error(), "public repository") {
		t.Errorf("unexpected error: %v", err)
	}
	if len(commands) != 0 {
		t.Errorf("it registered anyway: %v", commands)
	}
}

func TestSetupAllowsAPublicRepositoryWhenAsked(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	_, _, err := runCLI(t, setupOptions(t, false, &commands),
		"--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--dir", preUnpacked(t),
		"--allow-public", "--yes", "--skip-doctor")
	if err != nil {
		t.Fatalf("setup --allow-public: %v", err)
	}
	if len(commands) != 1 {
		t.Errorf("ran %v, want config.sh once", commands)
	}
}

// A bad name should stop before anything is downloaded or registered, not
// after GitHub has already been asked for a token.
func TestSetupRejectsABadRunnerNameBeforeTouchingAnything(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	_, _, err := runCLI(t, setupOptions(t, true, &commands),
		"--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--name", "bad name/here",
		"--yes", "--skip-doctor")
	if err == nil {
		t.Fatal("setup accepted an invalid runner name")
	}
	if len(commands) != 0 {
		t.Errorf("it got as far as running %v", commands)
	}
}

// An existing configuration is the operator's, possibly hand-edited and
// commented. Registering a runner must never rewrite it.
func TestSetupLeavesAnExistingConfigurationAlone(t *testing.T) {
	original := "# mine\ngithub:\n  repository: acme/widgets\n"
	cfg := configIn(t, original)

	var commands []string
	_, _, err := runCLI(t, setupOptions(t, true, &commands),
		"--config", cfg, "--token", "t",
		"setup", "--name", "runnerly-01", "--dir", preUnpacked(t), "--yes", "--skip-doctor")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	after, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("setup rewrote the configuration:\n%s", after)
	}
}
