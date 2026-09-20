package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/jobstate"
	"github.com/bablilayoub/runnerly/internal/state"
)

func TestReleaseVersion(t *testing.T) {
	tests := map[string]string{
		"actions-runner-linux-x64-2.337.0.tar.gz": "2.337.0",
		"actions-runner-osx-arm64-2.300.1.tar.gz": "2.300.1",
		"actions-runner-win-x64-2.337.0.zip":      "2.337.0",
		"something-else.tar.gz":                   "",
		"":                                        "",
	}
	for filename, want := range tests {
		if got := releaseVersion(filename); got != want {
			t.Errorf("releaseVersion(%q) = %q, want %q", filename, got, want)
		}
	}
}

// upgradeHandler serves the downloads endpoint at a given runner version,
// and reports whether Runnerly itself has a release.
func upgradeHandler(t *testing.T, runnerVersion string, runnerlyTag string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			if runnerlyTag == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"tag_name":"` + runnerlyTag + `"}`))

		case strings.HasSuffix(r.URL.Path, "/downloads"):
			_, _ = w.Write([]byte(`[{"os":"linux","architecture":"x64",` +
				`"filename":"actions-runner-linux-x64-` + runnerVersion + `.tar.gz",` +
				`"download_url":"http://127.0.0.1:1/x.tar.gz","sha256_checksum":"abc"}]`))

		case strings.HasSuffix(r.URL.Path, "/registration-token"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"AREGTOKEN","expires_at":"2099-01-01T00:00:00Z"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestUpgradeReportsAnOutOfDateRunner(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)
	if err := state.Put(state.Path(cfg), state.Runner{
		Name:    "runnerly-01",
		Scope:   repoScopeFor("acme", "widgets"),
		Dir:     dir,
		Release: "actions-runner-linux-x64-2.300.0.tar.gz",
	}); err != nil {
		t.Fatal(err)
	}

	var commands []string
	opts := []option{withGitHub(t, upgradeHandler(t, "2.337.0", "")), withRunnerEnv(&commands)}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	for _, want := range []string{"2.300.0", "2.337.0", "update available", "--runners"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Reporting only: nothing should have been touched.
	if len(commands) != 0 {
		t.Errorf("upgrade reinstalled without being asked: %v", commands)
	}
}

func TestUpgradeSaysNothingToCompareWithoutAReleaseHistory(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")

	var commands []string
	opts := []option{withGitHub(t, upgradeHandler(t, "2.337.0", "")), withRunnerEnv(&commands)}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	// Runnerly has published no releases, which is not the same as being
	// up to date.
	if !strings.Contains(out, "No releases have been published") {
		t.Errorf("output should say there is nothing to compare against:\n%s", out)
	}
}

func TestUpgradeReportsANewerRunnerly(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")

	var commands []string
	opts := []option{withGitHub(t, upgradeHandler(t, "2.337.0", "v9.9.9")), withRunnerEnv(&commands)}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !strings.Contains(out, "9.9.9") {
		t.Errorf("a newer Runnerly was not reported:\n%s", out)
	}
	// Runnerly does not replace its own binary, and says so rather than
	// implying an upgrade happened.
	if !strings.Contains(out, "does not replace its own binary") {
		t.Errorf("output should say what to do:\n%s", out)
	}
}

func TestUpgradeRunnersReinstallsAtTheCurrentRelease(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)
	if err := state.Put(state.Path(cfg), state.Runner{
		Name:    "runnerly-01",
		Scope:   repoScopeFor("acme", "widgets"),
		Dir:     dir,
		Release: "actions-runner-linux-x64-2.300.0.tar.gz",
	}); err != nil {
		t.Fatal(err)
	}

	var commands []string
	opts := []option{withGitHub(t, upgradeHandler(t, "2.337.0", "")), withRunnerEnv(&commands)}

	// The install would download, which the fake cannot serve, so this
	// checks the pre-install step: the old binaries must be cleared, or
	// the installer would reuse them and nothing would change.
	_, _, _ = runCLI(t, opts, "--config", cfg, "--token", "t", "upgrade", "--runners", "--yes")

	if _, err := os.Stat(filepath.Join(dir, "config.sh")); !os.IsNotExist(err) {
		t.Error("the old runner binaries were left in place, so the install would reuse them")
	}
}

func TestUpgradeSkipsARunnerMidJob(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)
	if err := state.Put(state.Path(cfg), state.Runner{
		Name:    "runnerly-01",
		Scope:   repoScopeFor("acme", "widgets"),
		Dir:     dir,
		Release: "actions-runner-linux-x64-2.300.0.tar.gz",
	}); err != nil {
		t.Fatal(err)
	}
	// An upgrade restarts the runner, which would throw this job away.
	if err := jobstate.Write(jobstate.Path(dir), jobstate.State{
		Status:     jobstate.StatusRunning,
		Repository: "acme/widgets",
		Workflow:   "test",
	}); err != nil {
		t.Fatal(err)
	}

	var commands []string
	opts := []option{withGitHub(t, upgradeHandler(t, "2.337.0", "")), withRunnerEnv(&commands)}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "upgrade", "--runners", "--yes")
	if err != nil {
		t.Fatalf("upgrade --runners: %v", err)
	}
	if !strings.Contains(out, "it is running") {
		t.Errorf("output should say why it was skipped:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "config.sh")); statErr != nil {
		t.Error("a runner mid-job was torn down anyway")
	}
}

func TestUpgradeRunnersNeedsConfirmation(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")
	dir := preUnpacked(t)
	if err := state.Put(state.Path(cfg), state.Runner{
		Name:    "runnerly-01",
		Scope:   repoScopeFor("acme", "widgets"),
		Dir:     dir,
		Release: "actions-runner-linux-x64-2.300.0.tar.gz",
	}); err != nil {
		t.Fatal(err)
	}

	var commands []string
	opts := []option{withGitHub(t, upgradeHandler(t, "2.337.0", "")), withRunnerEnv(&commands)}

	_, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "upgrade", "--runners")
	if err == nil {
		t.Fatal("upgrade proceeded without confirmation")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "config.sh")); statErr != nil {
		t.Error("the runner was torn down despite the refusal")
	}
}

func TestUpgradeWithNothingInstalled(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")

	var commands []string
	opts := []option{withGitHub(t, upgradeHandler(t, "2.337.0", "")), withRunnerEnv(&commands)}

	out, _, err := runCLI(t, opts, "--config", cfg, "--token", "t", "upgrade")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !strings.Contains(out, "no runners are installed") {
		t.Errorf("output = %q", out)
	}
	if !strings.Contains(out, "2.337.0") {
		t.Errorf("output should still say what GitHub expects:\n%s", out)
	}
}

// agentBinaryIsAbsolute guards the hook path assumption the upgrade path
// shares with the agent.
func TestAgentBinaryPathIsAbsolute(t *testing.T) {
	if got := agent.BinaryPath(); !filepath.IsAbs(got) {
		t.Errorf("BinaryPath() = %q, want an absolute path", got)
	}
}

// repoScopeFor builds a repository scope for a test fixture.
func repoScopeFor(owner, repo string) github.Scope {
	return github.Scope{Kind: github.KindRepository, Owner: owner, Repo: repo}
}
