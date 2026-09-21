package jobstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bablilayoub/runnerly/internal/docker"
)

func TestReadMissingFileIsIdle(t *testing.T) {
	state, err := Read(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if state.Running() {
		t.Error("a runner that has never run a job reported itself busy")
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runner", Dir, FileName)
	want := State{
		Status:     StatusRunning,
		Repository: "acme/widgets",
		Workflow:   "test",
		Job:        "build",
		RunID:      "12345",
		StartedAt:  time.Now().UTC().Truncate(time.Second),
		Docker: docker.Snapshot{
			Containers: []string{"c1"},
			Volumes:    []string{"v1"},
		},
	}

	if err := Write(path, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !got.Running() || got.Repository != "acme/widgets" || got.RunID != "12345" {
		t.Errorf("state = %+v", got)
	}
	if len(got.Docker.Containers) != 1 || got.Docker.Containers[0] != "c1" {
		t.Errorf("the docker snapshot did not survive: %+v", got.Docker)
	}
}

func TestWriteIsAtomic(t *testing.T) {
	// The agent reads this file while the hook writes it, with no locking
	// between two processes, so a partial read must be impossible.
	path := filepath.Join(t.TempDir(), FileName)
	if err := Write(path, State{Status: StatusRunning}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary file was left behind")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// See config_test.go: Windows has no permission bits.
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestCorruptFileReadsAsIdle(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := Read(path)
	if err == nil {
		t.Error("Read() hid a corrupt file")
	}
	// Whatever went wrong, the agent must not conclude a job is running for
	// ever.
	if state.Running() {
		t.Error("a corrupt file reported the runner as busy")
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{"never ran", State{Status: StatusIdle}, ""},
		{"finished", State{
			Status:     StatusIdle,
			Repository: "acme/widgets",
			Workflow:   "test",
			StartedAt:  time.Now().Add(-time.Minute),
			FinishedAt: time.Now(),
		}, "acme/widgets / test"},
		{"full", State{Status: StatusRunning, Repository: "acme/widgets", Workflow: "test"}, "acme/widgets / test"},
		{"repository only", State{Status: StatusRunning, Repository: "acme/widgets"}, "acme/widgets"},
		{"nothing known", State{Status: StatusRunning}, "a job"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.Describe(); got != tt.want {
				t.Errorf("Describe() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstallHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hooks are not implemented for Windows runners")
	}

	runnerDir := t.TempDir()
	hooks, err := InstallHooks(runnerDir, "/usr/local/bin/runnerly", "/etc/runnerly/config.yaml")
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}

	for _, path := range []string{hooks.Started, hooks.Completed} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("hook missing: %v", err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s is not executable: %o", path, info.Mode().Perm())
		}

		body, err := os.ReadFile(path) //nolint:gosec // a path this test just created
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.HasPrefix(text, "#!/bin/sh") {
			t.Errorf("%s has no shebang:\n%s", path, text)
		}
		// A hook that exits non-zero fails the job, so Runnerly's own
		// bookkeeping must never be able to do that.
		if !strings.Contains(text, "|| true") {
			t.Errorf("%s could fail a job:\n%s", path, text)
		}
		if !strings.Contains(text, "/usr/local/bin/runnerly") {
			t.Errorf("%s does not call the binary:\n%s", path, text)
		}
		if !strings.Contains(text, "/etc/runnerly/config.yaml") {
			t.Errorf("%s does not pass the configuration:\n%s", path, text)
		}
	}

	if !strings.Contains(hooks.Started, "job-started") || !strings.Contains(hooks.Completed, "job-completed") {
		t.Errorf("hooks = %+v", hooks)
	}
}

func TestHookEnvPointsTheRunnerAtThem(t *testing.T) {
	hooks := Hooks{Started: "/a/started.sh", Completed: "/a/completed.sh"}
	env := strings.Join(hooks.Env(), " ")

	if !strings.Contains(env, HookStartedEnv+"=/a/started.sh") {
		t.Errorf("env = %v", env)
	}
	if !strings.Contains(env, HookCompletedEnv+"=/a/completed.sh") {
		t.Errorf("env = %v", env)
	}
}

func TestInstallHooksQuotesAwkwardPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hooks are not implemented for Windows runners")
	}

	runnerDir := filepath.Join(t.TempDir(), "dir with spaces")
	if err := os.MkdirAll(runnerDir, 0o750); err != nil {
		t.Fatal(err)
	}

	hooks, err := InstallHooks(runnerDir, "/opt/my runnerly/runnerly", "/etc/conf.yaml")
	if err != nil {
		t.Fatalf("InstallHooks() error = %v", err)
	}
	body, err := os.ReadFile(hooks.Started) //nolint:gosec // a path this test just created
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `'/opt/my runnerly/runnerly'`) {
		t.Errorf("a path with a space was not quoted:\n%s", body)
	}
}

func TestInstallHooksNeedsABinaryPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hooks are not implemented for Windows runners")
	}
	if _, err := InstallHooks(t.TempDir(), "", "/etc/conf.yaml"); err == nil {
		t.Error("InstallHooks() accepted an empty binary path")
	}
}

func TestFromEnvironmentReadsWhatTheRunnerExports(t *testing.T) {
	env := map[string]string{
		"GITHUB_REPOSITORY": "acme/widgets",
		"GITHUB_WORKFLOW":   "test",
		"GITHUB_JOB":        "build",
		"GITHUB_RUN_ID":     "42",
	}
	state := FromEnvironment(func(k string) string { return env[k] })

	if state.Repository != "acme/widgets" || state.Workflow != "test" ||
		state.Job != "build" || state.RunID != "42" {
		t.Errorf("state = %+v", state)
	}
}

// The Windows hook is a .cmd, because a shell script is not a program on
// Windows and the runner starts a hook the way the system starts anything.
func TestInstallHooksWritesBatchOnWindows(t *testing.T) {
	dir := t.TempDir()

	hooks, err := installHooks("windows", dir, `C:\Program Files\runnerly\runnerly.exe`, "")
	if err != nil {
		t.Fatalf("installHooks: %v", err)
	}

	if filepath.Ext(hooks.Started) != ".cmd" || filepath.Ext(hooks.Completed) != ".cmd" {
		t.Errorf("hooks are %q and %q, want .cmd files", hooks.Started, hooks.Completed)
	}

	body, err := os.ReadFile(hooks.Started) //nolint:gosec // a path this test made
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"@echo off",
		`call "C:\Program Files\runnerly\runnerly.exe" agent hook started`,
		// The runner fails the job if a hook exits non-zero, so it must not.
		"exit /b 0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the hook is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "|| true") {
		t.Errorf("a shell idiom leaked into the batch hook:\n%s", text)
	}
}

// cmd expands %NAME% inside double quotes as happily as outside, and a
// Windows directory name may legally contain a percent sign. Left alone,
// the hook would be handed a path with a chunk of it replaced by an
// environment variable, or by nothing.
func TestBatchQuoteDoublesPercentSigns(t *testing.T) {
	got := batchQuote(`C:\builds\100%done\%USERNAME%`)
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Errorf("batchQuote() = %s, want it quoted", got)
	}
	// Three in, six out, and none of them left able to expand.
	if strings.Count(got, "%") != 6 {
		t.Errorf("batchQuote() = %s, want every percent doubled", got)
	}
	if got != `"C:\builds\100%%done\%%USERNAME%%"` {
		t.Errorf("batchQuote() = %s", got)
	}
}

// The hook is a `call` line, and `call` expands the line it was handed a
// second time. Four become two become one.
func TestBatchQuoteCalledSurvivesTwoExpansions(t *testing.T) {
	got := batchQuoteCalled(`C:\builds\100%done`)
	if got != `"C:\builds\100%%%%done"` {
		t.Errorf("batchQuoteCalled() = %s", got)
	}

	// What cmd does to it: each pass halves the run of percent signs.
	pass := func(s string) string { return strings.ReplaceAll(s, "%%", "%") }
	if final := pass(pass(got)); final != `"C:\builds\100%done"` {
		t.Errorf("after two expansions = %s, want the path back", final)
	}
}

func TestInstallHooksStillWritesShellElsewhere(t *testing.T) {
	dir := t.TempDir()

	hooks, err := installHooks("linux", dir, "/usr/local/bin/runnerly", "")
	if err != nil {
		t.Fatalf("installHooks: %v", err)
	}
	if filepath.Ext(hooks.Started) != ".sh" {
		t.Errorf("hook is %q, want a .sh", hooks.Started)
	}
}
