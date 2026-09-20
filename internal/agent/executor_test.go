package agent

import (
	"testing"

	"github.com/bablilayoub/runnerly/internal/config"
)

// TestConfigureEphemeralAlwaysInstallsHooks covers a real failure: an
// ephemeral run with executor.type: host ran a job, succeeded, and then
// reported "stopped without running a job".
//
// The hooks were installed only for the Docker executor, because their other
// job is Docker cleanup. But they are also the only way to tell a finished
// job from a crash, and that distinction is the whole lifecycle of a one-job
// runner.
func TestConfigureEphemeralAlwaysInstallsHooks(t *testing.T) {
	for _, executor := range []config.ExecutorType{config.ExecutorHost, config.ExecutorDocker} {
		t.Run(string(executor), func(t *testing.T) {
			cfg := config.Default()
			cfg.Executor.Type = executor

			hooks, _ := ConfigureEphemeral(cfg, "/etc/runnerly/config.yaml")
			if !hooks.Install {
				t.Error("an ephemeral runner cannot tell whether a job ran without the hooks")
			}
			if hooks.ConfigPath != "/etc/runnerly/config.yaml" {
				t.Errorf("ConfigPath = %q", hooks.ConfigPath)
			}
		})
	}
}

// A long-lived runner on the host executor has nothing to clean up, so it
// still gets no hooks. Only the ephemeral path needs them unconditionally.
func TestConfigureLeavesTheHostExecutorAlone(t *testing.T) {
	cfg := config.Default()
	cfg.Executor.Type = config.ExecutorHost

	hooks, extraEnv := Configure(cfg, "/etc/runnerly/config.yaml")
	if hooks.Install {
		t.Error("the host executor got hooks it has no use for")
	}
	if len(extraEnv) != 0 {
		t.Errorf("extraEnv = %v, want none", extraEnv)
	}
}

// Hooks installed purely for job state must not drag Docker in: the host
// executor gets no DOCKER_HOST even on the ephemeral path.
func TestConfigureEphemeralAddsNoDockerEnvOnHost(t *testing.T) {
	cfg := config.Default()
	cfg.Executor.Type = config.ExecutorHost
	cfg.Executor.Docker.Host = "unix:///run/user/1000/docker.sock"

	_, extraEnv := ConfigureEphemeral(cfg, "/etc/runnerly/config.yaml")
	if len(extraEnv) != 0 {
		t.Errorf("extraEnv = %v, want none on the host executor", extraEnv)
	}
}
