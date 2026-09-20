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
	"strings"
	"sync"
	"time"

	"github.com/bablilayoub/runnerly/internal/controlplane"
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
}

// Run supervises the runner until the context is canceled or the backoff
// policy gives up.
func Run(ctx context.Context, opts Options) error {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	script, err := Preflight(opts.Runner)
	if err != nil {
		return err
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

	// Reporting is wired in before the supervisor starts, so the first
	// transition is not missed.
	var report *reporter
	if opts.ControlPlane != nil {
		report = newReporter(opts.ControlPlane, log, opts.HeartbeatInterval)
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
			Stdout:  output,
			Stderr:  output,
		},
		Backoff:     opts.Backoff,
		Ephemeral:   opts.Runner.Ephemeral,
		StopTimeout: opts.StopTimeout,
		Start:       opts.Start,
		OnEvent:     onEvent,
	})
	if err != nil {
		return err
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

	if runErr != nil {
		log.Error("agent stopped", "event", "agent_failed", "error", runErr.Error())
		return runErr
	}
	log.Info("agent stopped", "event", "agent_stopped")
	return nil
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
