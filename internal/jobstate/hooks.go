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

// powershellHookScript is the Windows body of each hook.
//
// PowerShell and not batch, which is not a style choice: GitHub's runner
// supports exactly two kinds of hook and decides which by the extension.
// "Your script files must use a file extension for the relevant language,
// such as .sh or .ps1, in order to run successfully" — bash, invoked as
// `bash -e <path>`, and PowerShell, invoked as `pwsh -command ". '<path>'"`.
// A .cmd is not one of them and would simply never run.
//
// `exit 0` is the equivalent of the `|| true` above, and matters for the
// same reason: the runner fails the job if a hook exits non-zero, and
// Runnerly's bookkeeping going wrong must never fail somebody's build.
// The runner dot-sources this, so exit ends the PowerShell host with 0
// whatever the line before it did.
//
// `&` is the call operator, needed because the command is a quoted
// string rather than a bare word.
const powershellHookScript = `# Installed by Runnerly. Do not edit: it is rewritten whenever the agent
# starts.
#
# GitHub's runner runs this %s each job. It records what the runner is
# doing so the agent can report it, and cleans up Docker afterwards.
#
# A failure here must not fail the job, which is what the exit is for.
& %s agent hook %s --state %s --config %s
exit 0
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
// tested from either one. The Windows hook is PowerShell and cannot be
// run here; what can be checked from anywhere is that it is written and
// what it says. CI runs it on Windows.
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
		script, quote, suffix = powershellHookScript, powershellQuote, ".ps1"
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
		// ignores the mode; there the runner reads the extension and
		// hands the file to PowerShell.
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

// powershellQuote makes a path safe as one argument in a PowerShell
// script.
//
// Single quotes, because PowerShell expands nothing inside them: no
// $variable, no subexpression, no escape sequence. A single quote is the
// only character that ends the string early, and PowerShell's own escape
// for it is to double it. A single quote is legal in a Windows path, so
// this is not theoretical.
//
// Compare the shell version above, which has the same shape for the same
// reason — and the batch version this replaced, which needed percent
// signs doubled once for the parser and again for `call`. PowerShell has
// none of that, which is the other reason to be glad the runner wants it.
func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
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
