package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/state"
)

// setupHandler serves what setup asks GitHub for: who the token belongs to,
// whether the repository is private, a registration token, and the download
// listing.
func setupHandler(t *testing.T, private bool) http.HandlerFunc {
	return setupHandlerWith(t, private, false)
}

func setupHandlerWith(t *testing.T, private, manyRepos bool) http.HandlerFunc {
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
			if manyRepos {
				// More than the picker shows at once, which is the case
				// that used to be refused outright.
				var b strings.Builder
				b.WriteString("[")
				for i := 0; i < 60; i++ {
					if i > 0 {
						b.WriteString(",")
					}
					fmt.Fprintf(&b, `{"full_name":"acme/filler-%d","name":"filler-%d","private":true,`+
						`"owner":{"login":"acme"},"permissions":{"admin":true}}`, i, i)
				}
				b.WriteString(`,{"full_name":"acme/widgets","name":"widgets","private":true,` +
					`"owner":{"login":"acme"},"permissions":{"admin":true}}]`)
				_, _ = w.Write([]byte(b.String()))
				return
			}
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
		// Whichever one this machine can actually follow: pointing a Mac
		// at `agent systemd` is advice for a service manager it does not
		// have, and this is the line people read right after setup.
		"runnerly agent " + serviceGenerator() + " runnerly-01",
		"runs-on:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "AREGTOKEN") {
		t.Errorf("the registration token leaked into the output:\n%s", out)
	}
	for _, wrong := range []string{"systemd", "launchd", "schtasks"} {
		if wrong == serviceGenerator() {
			continue
		}
		if strings.Contains(out, "agent "+wrong) {
			t.Errorf("setup offered `agent %s` on %s:\n%s", wrong, runtime.GOOS, out)
		}
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

// An account with more repositories than the picker shows used to be
// refused here: "too many to list", go and pass --repo. That is a dead end
// in the one command whose purpose is not making you look things up, and
// it is what someone with a lot of repositories hits first.
//
// Typing a search now narrows the list instead.
func TestSetupSearchesWhenThereAreTooManyRepositories(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := []option{
		withGitHub(t, setupHandlerWith(t, true, true)),
		withRunnerEnv(&commands),
		withInteractive(),
		// Search for "widgets", then take the only match.
		withStdin("widgets\n1\ny\n"),
	}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--name", "runnerly-01", "--dir", preUnpacked(t), "--skip-doctor")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}

	if strings.Contains(out, "too many to list") {
		t.Errorf("it still refuses instead of searching:\n%s", out)
	}
	if !strings.Contains(out, "acme/widgets") {
		t.Errorf("the search did not surface the repository:\n%s", out)
	}
	if len(commands) != 1 {
		t.Fatalf("ran %v, want config.sh once", commands)
	}
	if !strings.Contains(commands[0], "acme/widgets") {
		t.Errorf("registered against the wrong repository:\n%s", commands[0])
	}
}

// Someone who already knows the name should not have to find it in a list.
func TestSetupAcceptsATypedRepositoryName(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := []option{
		withGitHub(t, setupHandlerWith(t, true, true)),
		withRunnerEnv(&commands),
		withInteractive(),
		withStdin("acme/widgets\ny\n"),
	}

	if _, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--name", "runnerly-01", "--dir", preUnpacked(t), "--skip-doctor"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if len(commands) != 1 || !strings.Contains(commands[0], "acme/widgets") {
		t.Errorf("did not register against the typed name: %v", commands)
	}
}

// A number outside the list is a mistake to correct, not a reason to exit
// and make someone start the whole wizard again.
func TestSetupReAsksOnABadNumber(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := []option{
		withGitHub(t, setupHandler(t, true)),
		withRunnerEnv(&commands),
		withInteractive(),
		withStdin("99\n1\ny\n"),
	}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--name", "runnerly-01", "--dir", preUnpacked(t), "--skip-doctor")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if !strings.Contains(out, "99 is not one of the numbers listed") {
		t.Errorf("it did not say why 99 was rejected:\n%s", out)
	}
	if len(commands) != 1 {
		t.Errorf("ran %v, want it to have carried on and registered once", commands)
	}
}

// Picking a public repository from the list used to end the command and
// tell you to start again with --allow-public — after you had already gone
// through the picker to get there.
//
// The refusal is right; ending the wizard is not the way to deliver it to
// someone who is standing there and can answer. Declining sends you back to
// the list.
func TestSetupOffersThePublicRepositoryChoiceInPlace(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := []option{
		withGitHub(t, setupHandler(t, false)), // acme/widgets is public
		withRunnerEnv(&commands),
		withInteractive(),
		// Pick it, agree to the warning, then confirm the plan.
		withStdin("1\ny\ny\n"),
	}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--name", "runnerly-01", "--dir", preUnpacked(t), "--skip-doctor")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}

	if !strings.Contains(out, "is a public repository") {
		t.Errorf("the warning was not shown:\n%s", out)
	}
	if !strings.Contains(out, "anyone who can get a workflow to run") {
		t.Errorf("the warning did not say why it matters:\n%s", out)
	}
	if len(commands) != 1 {
		t.Fatalf("ran %v, want config.sh once after agreeing", commands)
	}
}

// Declining must not register anything, and must not end the command
// either: it goes back to the list.
func TestSetupDecliningAPublicRepositoryReturnsToTheList(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := []option{
		withGitHub(t, setupHandler(t, false)),
		withRunnerEnv(&commands),
		withInteractive(),
		// Pick it, decline, then give nothing and let it stop.
		withStdin("1\nn\n\n"),
	}

	out, _, _ := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--name", "runnerly-01", "--dir", preUnpacked(t), "--skip-doctor")

	if len(commands) != 0 {
		t.Errorf("declining still registered: %v", commands)
	}
	if !strings.Contains(out, "Pick another") {
		t.Errorf("it did not offer the list again:\n%s", out)
	}
}

// The guard must not be softer than it was. A scope named on the command
// line has no list to go back to, so it is still refused outright — asking
// would be inventing a prompt where the operator already stated intent.
func TestSetupStillRefusesAPublicRepositoryNamedOutright(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	opts := []option{
		withGitHub(t, setupHandler(t, false)),
		withRunnerEnv(&commands),
		withInteractive(),
		withStdin("y\ny\ny\n"), // would agree to anything it was asked
	}

	_, _, err := runCLI(t, opts, "--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--name", "runnerly-01",
		"--dir", preUnpacked(t), "--skip-doctor")
	if err == nil {
		t.Fatal("a public repository named with --repo was accepted")
	}
	if !strings.Contains(err.Error(), "public repository") {
		t.Errorf("unexpected error: %v", err)
	}
	if len(commands) != 0 {
		t.Errorf("it registered anyway: %v", commands)
	}
}

// And --yes, which has nobody to ask, still requires the flag.
func TestSetupWithYesStillRequiresAllowPublic(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	var commands []string
	_, _, err := runCLI(t, setupOptions(t, false, &commands),
		"--config", cfg, "--token", "t",
		"setup", "--repo", "acme/widgets", "--yes", "--skip-doctor")
	if err == nil {
		t.Fatal("--yes accepted a public repository without --allow-public")
	}
	if len(commands) != 0 {
		t.Errorf("it registered anyway: %v", commands)
	}
}
