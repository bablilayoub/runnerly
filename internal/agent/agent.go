// Package agent supervises the official GitHub Actions runner on one machine.
//
// It is the piece that turns a registered runner into one that stays up: start
// run.sh, notice when it dies, bring it back with exponential backoff, and
// shut it down cleanly on SIGTERM.
//
// The agent does not talk to a Runnerly control plane. Enrollment and
// heartbeats are defined in the project plan but there is no server to send
// them to yet, so the agent reports through structured logs on stdout, which
// is what systemd and a log collector can already read.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bablilayoub/runnerly/internal/controlplane"
	"github.com/bablilayoub/runnerly/internal/jobstate"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/supervisor"
)

// Component is the value of the component field in every log line, so agent
// output can be filtered out of a mixed stream.
const Component = "runner-agent"

// Options configures an agent run.
type Options struct {
	// Runner is the installed runner to supervise.
	Runner state.Runner
	// Logger receives structured events. Nil discards them.
	Logger *slog.Logger
	// Backoff controls restarts. The zero value uses supervisor defaults.
	Backoff supervisor.Backoff
	// StopTimeout is how long a graceful stop is given before the runner is
	// killed. Zero uses the supervisor default.
	StopTimeout time.Duration
	// Output receives the runner's own stdout and stderr. Nil discards it.
	//
	// The runner's output is not structured, so it is kept separate from the
	// agent's own log rather than interleaved into it.
	Output io.Writer
	// Start launches the process. Nil uses the real one; tests supply a fake.
	Start supervisor.StartFunc
	// ControlPlane, when set, makes the agent report status and events to a
	// Runnerly server. Nil means the agent reports only through its log,
	// which is how it works with no control plane at all.
	ControlPlane *controlplane.Client
	// HeartbeatInterval is how often to report. Zero uses the default.
	HeartbeatInterval time.Duration
	// Hooks installs the job hooks that tell Runnerly when a job starts and
	// finishes. Without them the agent can see that the runner process is
	// alive but not whether it is doing anything.
	Hooks HookOptions
	// ExtraEnv is added to the runner's environment, for DOCKER_HOST and
	// anything else the executor needs.
	ExtraEnv []string
	// OnMachineToken persists a credential the control plane rotated.
	//
	// Nil means a rotated token is used for the rest of this run but not
	// written down, so the next start would find a credential that no
	// longer works and have to enroll again.
	OnMachineToken func(token string) error
}

// HookOptions describes the job hooks to install.
type HookOptions struct {
	// Install turns them on. They are the Docker executor's mechanism for
	// cleanup, and the only way the agent learns a runner is busy.
	Install bool
	// Binary is the runnerly executable the hooks call.
	Binary string
	// ConfigPath is passed to the hooks, so they resolve the same
	// configuration the agent did.
	ConfigPath string
}

// Outcome describes how a run ended.
type Outcome struct {
	// Completed is true when an ephemeral runner finished a job, as opposed
	// to exiting for any other reason. Only an ephemeral run sets it.
	Completed bool
	// Job is what the runner last did, when the job hooks recorded it.
	Job jobstate.State
}

// Run supervises the runner until the context is canceled or the backoff
// policy gives up.
func Run(ctx context.Context, opts Options) (Outcome, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	script, err := Preflight(opts.Runner)
	if err != nil {
		return Outcome{}, err
	}

	log := opts.Logger.With(
		"component", Component,
		"runner", opts.Runner.Name,
		"scope", opts.Runner.Scope.String(),
	)

	output := opts.Output
	if output == nil {
		output = io.Discard
	}

	// The runner's environment starts from this process's, so it keeps PATH
	// and everything else a build needs, and gains what Runnerly adds.
	runnerEnv := append(os.Environ(), opts.ExtraEnv...) //nolint:gocritic // a new slice is intended

	if opts.Hooks.Install {
		hooks, err := jobstate.InstallHooks(opts.Runner.Dir, opts.Hooks.Binary, opts.Hooks.ConfigPath)
		if err != nil {
			// Without hooks the runner still works; it just runs unobserved
			// and nothing cleans up after it. That is worth saying loudly
			// but not worth refusing to start over.
			log.Warn("could not install the job hooks, so jobs will run unobserved",
				"event", "hooks_unavailable", "error", err.Error())
		} else {
			runnerEnv = append(runnerEnv, hooks.Env()...)
			log.Info("installed the job hooks",
				"event", "hooks_installed", "dir", jobstate.StateDir(opts.Runner.Dir))
		}
	}

	// Reporting is wired in before the supervisor starts, so the first
	// transition is not missed.
	// The job hooks are the only source of truth for what the runner is
	// actually doing, so both busy reporting and ephemeral completion read
	// from the same place.
	statePath := jobstate.Path(opts.Runner.Dir)
	readJob := func() jobstate.State {
		state, err := jobstate.Read(statePath)
		if err != nil {
			log.Warn("could not read the job state",
				"event", "job_state_unreadable", "error", err.Error())
		}
		return state
	}

	var report *reporter
	if opts.ControlPlane != nil {
		report = newReporter(opts.ControlPlane, log, opts.HeartbeatInterval,
			runnerVersion(opts.Runner.Release))
		if opts.Hooks.Install {
			report.jobState = readJob
		}
		report.onToken = opts.OnMachineToken
	}

	onEvent := func(e supervisor.Event) {
		logEvent(log, e, opts.Runner.Ephemeral)
		if report != nil {
			report.observe(e)
		}
	}

	sup, err := supervisor.New(supervisor.Options{
		Process: supervisor.ProcessOptions{
			Dir:     opts.Runner.Dir,
			Command: script,
			Env:     runnerEnv,
			Stdout:  output,
			Stderr:  output,
		},
		Backoff:     opts.Backoff,
		Ephemeral:   opts.Runner.Ephemeral,
		StopTimeout: opts.StopTimeout,
		Start:       opts.Start,
		OnEvent:     onEvent,
		// An ephemeral runner's run.sh exits 0 whether it finished a job or
		// its listener was killed, so the exit code cannot be trusted to
		// mean "done". Ask the hooks whether a job actually ran.
		EphemeralDone: ephemeralDone(opts, readJob, log),
	})
	if err != nil {
		return Outcome{}, err
	}

	// The supervisor exists now, so a restart command has something to act
	// on. Wiring it after construction avoids a reference cycle between the
	// two.
	if report != nil {
		report.restart = sup.Restart
	}

	log.Info("agent started",
		"event", "agent_started",
		"dir", opts.Runner.Dir,
		"ephemeral", opts.Runner.Ephemeral,
		"labels", strings.Join(opts.Runner.Labels, ","),
		"reporting", report != nil,
	)

	// The reporter runs alongside supervision and is stopped after it, so a
	// final "offline" report goes out once the runner really has stopped.
	var reporting sync.WaitGroup
	if report != nil {
		reportCtx, stopReporting := context.WithCancel(context.Background())
		defer stopReporting()

		reporting.Add(1)
		go func() {
			defer reporting.Done()
			report.run(reportCtx)
		}()
		defer func() {
			stopReporting()
			reporting.Wait()
		}()
	}

	runErr := sup.Run(ctx)

	outcome := Outcome{Job: readJob()}
	// A completed run is one that ended on its own, cleanly, having done a
	// job. Cancellation and failure are neither.
	outcome.Completed = opts.Runner.Ephemeral && runErr == nil &&
		ctx.Err() == nil && outcome.Job.FinishedAJob()

	if runErr != nil {
		log.Error("agent stopped", "event", "agent_failed", "error", runErr.Error())
		return outcome, runErr
	}
	if outcome.Completed {
		log.Info("the ephemeral runner finished its job",
			"event", "job_completed",
			"job", outcome.Job.Describe(),
			"duration_seconds", outcome.Job.Duration().Seconds(),
		)
	}
	log.Info("agent stopped", "event", "agent_stopped")
	return outcome, nil
}

// ephemeralDone builds the predicate the supervisor uses to decide whether a
// clean exit finished the work.
//
// Without job hooks there is nothing better than the exit code, so the
// behavior is unchanged: any clean exit ends supervision. That is stated
// rather than silently assumed, because it is the case where an ephemeral
// runner can stop after a crash without having run anything.
func ephemeralDone(opts Options, readJob func() jobstate.State, log *slog.Logger) func() bool {
	if !opts.Runner.Ephemeral || !opts.Hooks.Install {
		return nil
	}
	return func() bool {
		job := readJob()
		if job.FinishedAJob() {
			return true
		}
		log.Warn("the runner exited cleanly without running a job, so it is being restarted",
			"event", "ephemeral_no_job",
			"hint", "an ephemeral runner that exits without work has usually lost its connection")
		return false
	}
}

// Preflight checks that a runner really is installed and ready, and returns
// the script that starts it.
//
// It is separate from Run so a command can report the same problems without
// launching anything.
func Preflight(r state.Runner) (script string, err error) {
	if r.Name == "" {
		return "", errors.New("the agent needs a runner name")
	}
	if r.Dir == "" {
		return "", fmt.Errorf("runner %q has no installation directory recorded", r.Name)
	}

	info, err := os.Stat(r.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("runner %q is recorded at %s, but that directory is gone.\n"+
				"Reinstall it with `runnerly runner create --name %s`, or forget it with "+
				"`runnerly runner remove %s`", r.Name, r.Dir, r.Name, r.Name)
		}
		return "", fmt.Errorf("inspect %s: %w", r.Dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", r.Dir)
	}

	if _, err := os.Stat(filepath.Join(r.Dir, ".runner")); err != nil {
		return "", fmt.Errorf("%s holds no configured runner.\n"+
			"config.sh writes .runner when registration succeeds; run "+
			"`runnerly runner create --name %s --replace` to register it again", r.Dir, r.Name)
	}

	for _, name := range []string{"run.sh", "run.cmd"} {
		candidate := filepath.Join(r.Dir, name)
		if _, err := os.Stat(candidate); err == nil {
			// The supervisor runs it with the directory as its working
			// directory, and the runner expects to be invoked that way.
			return "." + string(os.PathSeparator) + name, nil
		}
	}
	return "", fmt.Errorf("%s holds no run.sh.\nThe install looks incomplete; "+
		"reinstall with `runnerly runner create --name %s --replace`", r.Dir, r.Name)
}

// logEvent turns a supervisor event into one structured log line.
//
// The severity is the operator's cue: a restart is a warning because something
// went wrong, and giving up is an error because nothing will retry.
func logEvent(log *slog.Logger, e supervisor.Event, ephemeral bool) {
	switch e.Kind {
	case supervisor.EventStarting:
		log.Debug("starting the runner", "event", "runner_starting", "attempt", e.Attempt)

	case supervisor.EventStarted:
		log.Info("runner is running", "event", "runner_online", "pid", e.PID)

	case supervisor.EventExited:
		attrs := []any{
			"event", "runner_exited",
			"exit_code", e.ExitCode,
			"uptime_seconds", e.Uptime.Seconds(),
			"attempt", e.Attempt,
		}
		if e.Err != nil {
			attrs = append(attrs, "error", e.Err.Error())
		}
		// A long-lived runner should never exit on its own, so any exit is
		// worth a warning — including a clean one. Killing the runner's
		// listener makes run.sh exit 0, so treating code 0 as normal would
		// log a crash at INFO and hide it from anyone filtering on severity.
		// Only an ephemeral runner is expected to finish.
		if ephemeral && e.ExitCode == 0 {
			log.Info("runner finished its job", attrs...)
		} else {
			log.Warn("runner exited unexpectedly", attrs...)
		}

	case supervisor.EventRestarting:
		log.Warn("restarting the runner",
			"event", "runner_restarting",
			"attempt", e.Attempt,
			"delay_seconds", e.Delay.Seconds(),
		)

	case supervisor.EventHealthy:
		log.Info("runner is healthy again",
			"event", "runner_healthy",
			"uptime_seconds", e.Uptime.Seconds(),
		)

	case supervisor.EventGaveUp:
		log.Error("giving up on the runner",
			"event", "runner_failed",
			"attempts", e.Attempt,
		)

	case supervisor.EventStopping:
		attrs := []any{"event", "runner_stopping", "pid", e.PID}
		if e.Err != nil {
			attrs = append(attrs, "error", e.Err.Error())
		}
		log.Info("stopping the runner", attrs...)

	case supervisor.EventKilled:
		log.Warn("killed the runner after a graceful stop timed out",
			"event", "runner_killed",
			"pid", e.PID,
			"timeout_seconds", e.Delay.Seconds(),
		)

	case supervisor.EventStopped:
		log.Info("runner is stopped", "event", "runner_offline", "pid", e.PID)
	}
}

// releaseVersion pulls the version out of the archive name GitHub published,
// which is the only place the installed version is written down: the runner
// ships no version file of its own, and its .runner config does not carry
// one either.
//
// actions-runner-osx-arm64-2.337.0.tar.gz -> 2.337.0
var releaseVersion = regexp.MustCompile(`-(\d+\.\d+\.\d+)\.tar\.gz$`)

// runnerVersion reports the installed runner's version, or "" when it cannot
// be told. An empty string means "unknown" everywhere it is shown, which is
// better than a number that might be wrong.
func runnerVersion(release string) string {
	m := releaseVersion.FindStringSubmatch(release)
	if m == nil {
		return ""
	}
	return m[1]
}
