package jobstate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Environment variables GitHub's runner reads to find the job hooks.
const (
	// HookStartedEnv names the script run before each job.
	HookStartedEnv = "ACTIONS_RUNNER_HOOK_JOB_STARTED"
	// HookCompletedEnv names the script run after each job.
	HookCompletedEnv = "ACTIONS_RUNNER_HOOK_JOB_COMPLETED"
)

// Hooks describes the scripts installed into a runner directory.
type Hooks struct {
	Started   string
	Completed string
}

// Env returns the variables that point the runner at these hooks.
func (h Hooks) Env() []string {
	return []string{
		HookStartedEnv + "=" + h.Started,
		HookCompletedEnv + "=" + h.Completed,
	}
}

// hookScript is the body of each hook.
//
// It is a one-liner that hands over to the Runnerly binary, so the logic
// lives in Go where it can be tested rather than in shell that cannot.
//
// The trailing `|| true` matters: the runner fails the job if a hook exits
// non-zero. Runnerly's bookkeeping going wrong must never fail somebody's
// build, so a failure here is recorded on stderr and swallowed. The command
// exits 0 on its own too; this is the second belt.
const hookScript = `#!/bin/sh
# Installed by Runnerly. Do not edit: it is rewritten whenever the agent
# starts.
#
# GitHub's runner runs this %s each job. It records what the runner is doing
# so the agent can report it, and cleans up Docker afterwards.
#
# A failure here must not fail the job, which is what the || true is for.
%s agent hook %s --state %s --config %s || true
`

// InstallHooks writes the hook scripts into a runner directory and returns
// where they went.
//
// They are rewritten every time rather than created once: the binary may
// have moved, the configuration path may have changed, and a stale hook
// pointing at neither would silently stop recording anything.
func InstallHooks(runnerDir, binary, configPath string) (Hooks, error) {
	if runtime.GOOS == "windows" {
		// The hook would need to be a .cmd, and nothing else in Runnerly
		// supports Windows runners yet. Saying so beats writing a shell
		// script the runner cannot execute.
		return Hooks{}, fmt.Errorf("job hooks are not implemented for Windows runners")
	}
	if binary == "" {
		return Hooks{}, fmt.Errorf("the hooks need the path to the runnerly binary")
	}

	dir := StateDir(runnerDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Hooks{}, fmt.Errorf("create %s: %w", dir, err)
	}

	statePath := Path(runnerDir)
	hooks := Hooks{
		Started:   filepath.Join(dir, "job-started.sh"),
		Completed: filepath.Join(dir, "job-completed.sh"),
	}

	for name, path := range map[string]string{"started": hooks.Started, "completed": hooks.Completed} {
		when := "before"
		if name == "completed" {
			when = "after"
		}
		body := fmt.Sprintf(hookScript, when,
			shellQuote(binary), name, shellQuote(statePath), shellQuote(configPath))

		// 0700, not 0600: the runner has to execute this. It is owner-only,
		// which is what matters — it runs as the runner's user and nobody
		// else may write to something that will be executed.
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil { //nolint:gosec // G306: a hook must be executable
			return Hooks{}, fmt.Errorf("write %s: %w", path, err)
		}
	}
	return hooks, nil
}

// shellQuote makes a path safe inside single quotes in the hook script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// FromEnvironment reads what the runner tells a hook about the current job.
//
// The runner exports the usual GITHUB_* variables to hook scripts, which is
// where the job's identity comes from; none of it is guessed.
func FromEnvironment(getenv func(string) string) State {
	if getenv == nil {
		getenv = os.Getenv
	}
	return State{
		Repository: getenv("GITHUB_REPOSITORY"),
		Workflow:   getenv("GITHUB_WORKFLOW"),
		Job:        getenv("GITHUB_JOB"),
		RunID:      getenv("GITHUB_RUN_ID"),
	}
}
