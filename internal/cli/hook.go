package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/docker"
	"github.com/bablilayoub/runnerly/internal/jobstate"
)

// newHookCommand is what the job hook scripts call.
//
// It is hidden: an operator never types this. It exists so the logic lives
// in Go, where it is tested, instead of in a shell script that cannot be.
//
// Every path through it exits 0. GitHub's runner fails the job when a hook
// exits non-zero, and Runnerly's own bookkeeping going wrong must not fail
// somebody's build. Problems go to stderr, which lands in the job log where
// whoever is debugging will see it.
func newHookCommand(e *env) *cobra.Command {
	var statePath string

	cmd := &cobra.Command{
		Use:    "hook <started|completed>",
		Short:  "Called by the GitHub runner's job hooks",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if statePath == "" {
				fmt.Fprintln(e.errOut, "runnerly: the hook needs --state; nothing was recorded")
				return nil
			}

			cfg, _, _, err := e.loadConfig()
			if err != nil {
				// Without configuration there is nothing safe to do, but the
				// job must still run.
				fmt.Fprintf(e.errOut, "runnerly: could not read the configuration: %v\n", err)
				return nil
			}

			switch args[0] {
			case "started":
				e.hookStarted(cmd.Context(), cfg, statePath)
			case "completed":
				e.hookCompleted(cmd.Context(), cfg, statePath)
			default:
				fmt.Fprintf(e.errOut, "runnerly: %q is not a hook\n", args[0])
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&statePath, "state", "", "path to the job state file")
	return cmd
}

// hookStarted records the job and, for the Docker executor, what Docker held
// before it ran.
func (e *env) hookStarted(ctx context.Context, cfg config.Config, statePath string) {
	state := jobstate.FromEnvironment(nil)
	state.Status = jobstate.StatusRunning
	state.StartedAt = time.Now().UTC()

	if cfg.CleanupAfterJob() {
		// The snapshot is what makes cleanup safe: afterwards only what
		// appeared since is removed, so anything else on the machine stays.
		snapshot, err := e.dockerClient(cfg).Take(ctx)
		if err != nil {
			fmt.Fprintf(e.errOut,
				"runnerly: could not record the Docker state, so nothing will be cleaned up after this job: %v\n", err)
		} else {
			state.Docker = snapshot
		}
	}

	if err := jobstate.Write(statePath, state); err != nil {
		fmt.Fprintf(e.errOut, "runnerly: could not record the job: %v\n", err)
	}
}

// hookCompleted cleans up after the job and marks the runner idle.
func (e *env) hookCompleted(ctx context.Context, cfg config.Config, statePath string) {
	previous, err := jobstate.Read(statePath)
	if err != nil {
		fmt.Fprintf(e.errOut, "runnerly: could not read the job state: %v\n", err)
	}

	if cfg.CleanupAfterJob() {
		e.cleanupAfterJob(ctx, cfg, previous)
	}

	// Mark idle whatever happened above: a cleanup problem must not leave
	// the runner looking permanently busy.
	if err := jobstate.Write(statePath, jobstate.State{
		Status:     jobstate.StatusIdle,
		Repository: previous.Repository,
		Workflow:   previous.Workflow,
		Job:        previous.Job,
		RunID:      previous.RunID,
		StartedAt:  previous.StartedAt,
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		fmt.Fprintf(e.errOut, "runnerly: could not clear the job state: %v\n", err)
	}
}

func (e *env) cleanupAfterJob(ctx context.Context, cfg config.Config, previous jobstate.State) {
	client := e.dockerClient(cfg)

	after, err := client.Take(ctx)
	if err != nil {
		fmt.Fprintf(e.errOut, "runnerly: could not inspect Docker, so nothing was cleaned up: %v\n", err)
		return
	}

	added := docker.Added(previous.Docker, after)
	if added.Empty() {
		return
	}

	result := client.Remove(ctx, added, cfg.Executor.Docker.PruneImages)
	if result.Removed() > 0 {
		fmt.Fprintf(e.errOut, "runnerly: cleaned up %d container(s), %d volume(s) and %d network(s) this job created\n",
			result.Containers, result.Volumes, result.Networks)
	}
	for _, problem := range result.Problems {
		fmt.Fprintf(e.errOut, "runnerly: could not remove %s\n", problem)
	}
}

// dockerClient builds a Docker client from the configuration.
func (e *env) dockerClient(cfg config.Config) *docker.Client {
	if e.newDockerClient != nil {
		return e.newDockerClient(cfg)
	}
	return docker.New(docker.DefaultEnv(cfg.Executor.Docker.Host, cfg.Executor.Docker.Timeout.Duration()))
}
