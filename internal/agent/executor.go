package agent

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/bablilayoub/runnerly/internal/config"
)

// Configure derives what the executor needs from the configuration: the job
// hooks to install, and anything to add to the runner's environment.
//
// Both entry points — the CLI's `agent run` and the runnerly-agent daemon —
// go through this, so they cannot drift into setting up the runner
// differently.
func Configure(cfg config.Config, configPath string) (HookOptions, []string) {
	return configure(cfg, configPath, false)
}

// ConfigureEphemeral is Configure for a one-job runner, which needs the job
// hooks whatever the executor is.
//
// The hooks do two jobs, and only one of them is about Docker. They clean up
// after a job, which the host executor has no use for — and they are also
// the only way to tell a finished job from a crash, which is the entire
// lifecycle of a one-job runner. Installing them only for Docker meant
// `ephemeral run` with executor.type: host ran a job successfully and then
// reported "stopped without running a job".
func ConfigureEphemeral(cfg config.Config, configPath string) (HookOptions, []string) {
	return configure(cfg, configPath, true)
}

func configure(cfg config.Config, configPath string, needJobState bool) (HookOptions, []string) {
	var hooks HookOptions
	var extraEnv []string

	docker := cfg.Executor.Type == config.ExecutorDocker

	if docker || needJobState {
		hooks = HookOptions{
			Install:    true,
			Binary:     BinaryPath(),
			ConfigPath: configPath,
		}
	}

	// The host executor runs jobs directly: nothing to clean up, and no
	// container environment to point the runner at. The hook handler checks
	// the same setting before it touches Docker, so a hook installed here
	// for its job state alone does no Docker work.
	if docker {
		if host := cfg.Executor.Docker.Host; host != "" {
			// The runner starts job containers itself, so it needs to reach
			// the same daemon Runnerly does.
			extraEnv = append(extraEnv, "DOCKER_HOST="+host)
		}
	}
	return hooks, extraEnv
}

// BinaryPath finds the runnerly executable for the job hooks to call.
//
// The hooks run later, from the runner, with no guarantee about the working
// directory, so this resolves to an absolute path now rather than relying on
// PATH at job time.
func BinaryPath() string {
	const name = "runnerly"

	if self, err := os.Executable(); err == nil {
		// The CLI is itself `runnerly` when invoked as `runnerly agent run`.
		if filepath.Base(self) == name {
			return self
		}
		// The daemon usually sits beside it.
		candidate := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return "/usr/local/bin/" + name
}
