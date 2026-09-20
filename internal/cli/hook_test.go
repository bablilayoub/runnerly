package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/docker"
	"github.com/bablilayoub/runnerly/internal/jobstate"
)

// withDocker points the hook at a fake Docker daemon.
func withDocker(calls *[]string, listings map[string]string, failures map[string]error) option {
	return func(e *env) {
		e.newDockerClient = func(config.Config) *docker.Client {
			return docker.New(docker.Env{
				Run: func(_ context.Context, args ...string) ([]byte, error) {
					command := strings.Join(args, " ")
					*calls = append(*calls, command)
					for prefix, err := range failures {
						if strings.HasPrefix(command, prefix) {
							return []byte("error"), err
						}
					}
					for prefix, out := range listings {
						if strings.HasPrefix(command, prefix) {
							return []byte(out), nil
						}
					}
					return nil, nil
				},
			})
		}
	}
}

const dockerConfig = "executor:\n  type: docker\n  docker:\n    cleanup: after_job\n"

func TestHookRecordsTheJobAndSnapshotsDocker(t *testing.T) {
	cfg := configIn(t, dockerConfig)
	statePath := filepath.Join(t.TempDir(), "job.json")

	t.Setenv("GITHUB_REPOSITORY", "acme/widgets")
	t.Setenv("GITHUB_WORKFLOW", "test")

	var calls []string
	opts := []option{withDocker(&calls, map[string]string{
		"ps --all":   "existing\n",
		"volume ls":  "cache\n",
		"network ls": "bridge\n",
	}, nil)}

	if _, _, err := runCLI(t, opts, "--config", cfg, "agent", "hook", "started", "--state", statePath); err != nil {
		t.Fatalf("hook started: %v", err)
	}

	state, err := jobstate.Read(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Running() {
		t.Error("the job was not recorded as running")
	}
	if state.Repository != "acme/widgets" || state.Workflow != "test" {
		t.Errorf("state = %+v", state)
	}
	if len(state.Docker.Containers) != 1 {
		t.Errorf("the pre-job Docker state was not recorded: %+v", state.Docker)
	}
}

func TestHookCleansUpOnlyWhatTheJobCreated(t *testing.T) {
	cfg := configIn(t, dockerConfig)
	statePath := filepath.Join(t.TempDir(), "job.json")

	// Before the job: one container that belongs to something else.
	var calls []string
	before := []option{withDocker(&calls, map[string]string{
		"ps --all":   "bystander\n",
		"volume ls":  "",
		"network ls": "bridge\n",
	}, nil)}
	if _, _, err := runCLI(t, before, "--config", cfg, "agent", "hook", "started", "--state", statePath); err != nil {
		t.Fatal(err)
	}

	// After the job: the bystander plus what the job made.
	calls = nil
	after := []option{withDocker(&calls, map[string]string{
		"ps --all":   "bystander\njob-container\n",
		"volume ls":  "job-volume\n",
		"network ls": "bridge\njob-network\n",
	}, nil)}
	out, _, err := runCLI(t, after, "--config", cfg, "agent", "hook", "completed", "--state", statePath)
	if err != nil {
		t.Fatalf("hook completed: %v", err)
	}
	_ = out

	joined := strings.Join(calls, "\n")
	for _, want := range []string{"rm --force --volumes job-container", "volume rm --force job-volume", "network rm job-network"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the job's %q was not removed:\n%s", want, joined)
		}
	}
	// The whole point: someone else's container survives.
	if strings.Contains(joined, "bystander") {
		t.Errorf("cleanup touched a container that was there before the job:\n%s", joined)
	}

	state, err := jobstate.Read(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Running() {
		t.Error("the runner is still marked busy after the job finished")
	}
}

func TestHookDoesNotCleanUpForTheHostExecutor(t *testing.T) {
	cfg := configIn(t, "executor:\n  type: host\n")
	statePath := filepath.Join(t.TempDir(), "job.json")

	var calls []string
	opts := []option{withDocker(&calls, nil, nil)}

	for _, hook := range []string{"started", "completed"} {
		if _, _, err := runCLI(t, opts, "--config", cfg, "agent", "hook", hook, "--state", statePath); err != nil {
			t.Fatalf("hook %s: %v", hook, err)
		}
	}
	if len(calls) != 0 {
		t.Errorf("the host executor talked to Docker: %v", calls)
	}

	// It still tracks the job, which is what reports busy.
	state, err := jobstate.Read(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.FinishedAt.IsZero() {
		t.Error("the job was not recorded for the host executor")
	}
}

func TestCleanupCanBeTurnedOff(t *testing.T) {
	cfg := configIn(t, "executor:\n  type: docker\n  docker:\n    cleanup: never\n")
	statePath := filepath.Join(t.TempDir(), "job.json")

	var calls []string
	opts := []option{withDocker(&calls, map[string]string{"ps --all": "c1\n"}, nil)}

	for _, hook := range []string{"started", "completed"} {
		if _, _, err := runCLI(t, opts, "--config", cfg, "agent", "hook", hook, "--state", statePath); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 0 {
		t.Errorf("cleanup ran though it is turned off: %v", calls)
	}
}

// TestHookNeverFailsAJob is the property that matters most here. GitHub's
// runner fails the job when a hook exits non-zero, so Runnerly's own
// bookkeeping going wrong must never take somebody's build down with it.
func TestHookNeverFailsAJob(t *testing.T) {
	cfg := configIn(t, dockerConfig)
	statePath := filepath.Join(t.TempDir(), "job.json")

	var calls []string
	broken := []option{withDocker(&calls, nil, map[string]error{
		"ps": errors.New("cannot connect to the Docker daemon"),
	})}

	cases := []struct {
		name string
		opts []option
		args []string
	}{
		{"daemon down on start", broken, []string{"agent", "hook", "started", "--state", statePath}},
		{"daemon down on finish", broken, []string{"agent", "hook", "completed", "--state", statePath}},
		{"no state path", nil, []string{"agent", "hook", "started"}},
		{"unknown hook", nil, []string{"agent", "hook", "sideways", "--state", statePath}},
		{"unwritable state", nil, []string{"agent", "hook", "started", "--state", "/proc/nope/job.json"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--config", cfg}, tt.args...)
			if _, _, err := runCLI(t, tt.opts, args...); err != nil {
				t.Errorf("the hook returned %v; it must never fail a job", err)
			}
		})
	}
}

func TestACleanupFailureStillClearsTheJob(t *testing.T) {
	cfg := configIn(t, dockerConfig)
	statePath := filepath.Join(t.TempDir(), "job.json")

	var calls []string
	if _, _, err := runCLI(t, []option{withDocker(&calls, map[string]string{"ps --all": "before\n"}, nil)},
		"--config", cfg, "agent", "hook", "started", "--state", statePath); err != nil {
		t.Fatal(err)
	}

	broken := []option{withDocker(&calls, nil, map[string]error{
		"ps": errors.New("daemon went away"),
	})}
	if _, _, err := runCLI(t, broken, "--config", cfg, "agent", "hook", "completed", "--state", statePath); err != nil {
		t.Fatal(err)
	}

	// A cleanup problem must not leave the runner looking busy for ever.
	state, err := jobstate.Read(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Running() {
		t.Error("the runner is stuck busy after a cleanup failure")
	}
}

func TestHookIsHiddenFromHelp(t *testing.T) {
	out, _, err := run(t, "agent", "--help")
	if err != nil {
		t.Fatal(err)
	}
	// An operator never types this; it exists for the runner to call.
	if strings.Contains(out, "hook") {
		t.Errorf("the hook command is listed in help:\n%s", out)
	}
}

func TestHookWritesStateUnderTheRunnerDirectory(t *testing.T) {
	// The path the agent hands the hooks must be the one they write, so the
	// agent reads back what the hooks recorded.
	runnerDir := t.TempDir()
	want := filepath.Join(runnerDir, ".runnerly", "job.json")
	if got := jobstate.Path(runnerDir); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Dir(want)); err == nil {
		t.Error("the state directory should not exist before anything writes to it")
	}
}
