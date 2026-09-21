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

// batchHookScript is the Windows body of each hook.
//
// The runner is a .NET program and starts a hook the way the operating
// system starts anything: a shell script is not a program on Windows, so
// this is a .cmd that does the same one thing the shell version does.
//
// `exit /b 0` is the batch equivalent of the `|| true` above, and matters
// for the same reason: the runner fails the job if a hook exits non-zero,
// and Runnerly's bookkeeping going wrong must never fail somebody's build.
//
// `call` is what makes that true. Without it, a batch file that runs
// another batch file hands over control and never gets it back: the exit
// line is not reached, and the hook returns whatever the other file
// returned. Runnerly is normally an .exe, where it would make no
// difference — but a .cmd wrapper is exactly the sort of thing an
// operator puts in front of it, and this failing then would fail builds
// rather than anything visible here. Found by running it, not reading it.
const batchHookScript = `@echo off
rem Installed by Runnerly. Do not edit: it is rewritten whenever the agent
rem starts.
rem
rem GitHub's runner runs this %s each job. It records what the runner is
rem doing so the agent can report it, and cleans up Docker afterwards.
rem
rem A failure here must not fail the job, which is what the exit is for.
call %s agent hook %s --state %s --config %s
exit /b 0
`

// InstallHooks writes the hook scripts into a runner directory and returns
// where they went.
//
// They are rewritten every time rather than created once: the binary may
// have moved, the configuration path may have changed, and a stale hook
// pointing at neither would silently stop recording anything.
func InstallHooks(runnerDir, binary, configPath string) (Hooks, error) {
	return installHooks(runtime.GOOS, runnerDir, binary, configPath)
}

// installHooks takes the platform as an argument so both shapes can be
// tested from either one. The Windows hook is a .cmd and cannot be
// executed here to check it; what can be checked is that it is written,
// and that a path with a character batch treats specially comes out of it
// still meaning the same path.
func installHooks(goos, runnerDir, binary, configPath string) (Hooks, error) {
	if binary == "" {
		return Hooks{}, fmt.Errorf("the hooks need the path to the runnerly binary")
	}

	dir := StateDir(runnerDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Hooks{}, fmt.Errorf("create %s: %w", dir, err)
	}

	script, quote, suffix := hookScript, shellQuote, ".sh"
	if goos == "windows" {
		script, quote, suffix = batchHookScript, batchQuoteCalled, ".cmd"
	}

	statePath := Path(runnerDir)
	hooks := Hooks{
		Started:   filepath.Join(dir, "job-started"+suffix),
		Completed: filepath.Join(dir, "job-completed"+suffix),
	}

	for name, path := range map[string]string{"started": hooks.Started, "completed": hooks.Completed} {
		when := "before"
		if name == "completed" {
			when = "after"
		}
		body := fmt.Sprintf(script, when,
			quote(binary), name, quote(statePath), quote(configPath))

		// 0700, not 0600: the runner has to execute this. It is owner-only,
		// which is what matters — it runs as the runner's user and nobody
		// else may write to something that will be executed. Windows
		// ignores the mode and decides by the .cmd extension.
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

// batchQuote makes a path safe as one argument on an ordinary batch line.
//
// Double quotes handle spaces and the characters cmd treats as operators.
// A percent sign is the one they do not handle: cmd expands it inside
// quotes as happily as outside, and a Windows directory name may legally
// contain one. Doubling is how a batch file writes a literal percent, and
// a lone one that expands to nothing is simply deleted — which is how
// C:\builds\100%conf\x arrives as C:\builds\100conf\x.
//
// A double quote cannot appear in a Windows path at all, so there is
// nothing to escape and nothing to smuggle in through one.
func batchQuote(s string) string {
	return `"` + strings.ReplaceAll(s, "%", "%%") + `"`
}

// batchQuoteCalled is batchQuote for a line that starts with `call`.
//
// `call` re-parses the line it was handed, so everything on it is percent
// expanded twice: once by the batch parser reading the file, and once by
// call reading what the parser produced. Four percent signs become two
// become one.
//
// This is not theoretical. The hook is a `call` line, it was quoted for a
// single pass, and a config path containing a percent arrived at the
// binary with that percent and the character after it missing. The
// Windows job caught it by running the hook and reading back the
// arguments the other side actually received.
func batchQuoteCalled(s string) string {
	return `"` + strings.ReplaceAll(s, "%", "%%%%") + `"`
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
