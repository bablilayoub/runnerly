// Package supervisor keeps a child process running.
//
// Runnerly uses it for GitHub's official runner: start run.sh, notice when it
// exits, and bring it back with exponential backoff until it either stays up
// or has clearly failed for good. The supervisor knows nothing about GitHub or
// about runners; it supervises a command.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"
)

// ErrGaveUp means the process failed more times in a row than the backoff
// policy allows. The supervisor stops rather than restarting forever, because
// a runner that cannot start is a problem to surface, not to hide.
var ErrGaveUp = errors.New("the process failed too many times in a row")

// EventKind identifies what happened.
type EventKind string

const (
	// EventStarting is emitted before each launch attempt.
	EventStarting EventKind = "starting"
	// EventStarted is emitted once the process is running.
	EventStarted EventKind = "started"
	// EventExited is emitted when the process exits on its own.
	EventExited EventKind = "exited"
	// EventRestarting is emitted while waiting out the backoff delay.
	EventRestarting EventKind = "restarting"
	// EventHealthy is emitted when a process has run long enough that its
	// failure history no longer counts against it.
	EventHealthy EventKind = "healthy"
	// EventGaveUp is emitted when the supervisor stops retrying.
	EventGaveUp EventKind = "gave_up"
	// EventStopping is emitted when a shutdown has been requested.
	EventStopping EventKind = "stopping"
	// EventKilled is emitted when a graceful stop timed out.
	EventKilled EventKind = "killed"
	// EventStopped is emitted once the process is gone.
	EventStopped EventKind = "stopped"
	// EventCompleted is emitted when an ephemeral process finished its work,
	// as distinct from merely exiting.
	EventCompleted EventKind = "completed"
)

// Event describes a change in the supervised process.
type Event struct {
	Kind EventKind
	// Attempt counts consecutive restarts. It is 0 while healthy.
	Attempt int
	PID     int
	// ExitCode is meaningful for EventExited: 0 for a clean exit, -1 when the
	// process was signaled.
	ExitCode int
	// Uptime is how long the process ran, for EventExited.
	Uptime time.Duration
	// Delay is how long the supervisor will wait, for EventRestarting.
	Delay time.Duration
	// Err carries the underlying failure, if any.
	Err error
}

// Backoff controls how restarts are spaced.
type Backoff struct {
	// Initial is the delay after the first failure.
	Initial time.Duration
	// Max caps the delay.
	Max time.Duration
	// Factor multiplies the delay after each further failure.
	Factor float64
	// MaxRestarts is how many times in a row to restart a failing process
	// before giving up. Zero means never give up.
	//
	// It counts restarts, not launches: with the default of 5 the process is
	// started once and then restarted up to five times, which is the 5s, 10s,
	// 20s, 40s, 80s schedule.
	MaxRestarts int
	// ResetAfter is how long a process must run before its history is
	// forgiven. Without it, a runner that works fine for weeks would still be
	// one failure away from the end of its backoff.
	ResetAfter time.Duration
}

// DefaultBackoff is 5s, 10s, 20s, 40s, 80s, then stop. A process that stays up
// for a minute is treated as healthy again.
func DefaultBackoff() Backoff {
	return Backoff{
		Initial:     5 * time.Second,
		Max:         80 * time.Second,
		Factor:      2,
		MaxRestarts: 5,
		ResetAfter:  time.Minute,
	}
}

// Delay returns how long to wait before the given attempt, counting from 1.
func (b Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if b.Initial <= 0 {
		return 0
	}
	factor := b.Factor
	if factor < 1 {
		factor = 1
	}

	d := float64(b.Initial) * math.Pow(factor, float64(attempt-1))
	if b.Max > 0 && d > float64(b.Max) {
		return b.Max
	}
	// An overflowed duration is capped rather than wrapping negative.
	if d > float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(d)
}

// Options configures a Supervisor.
type Options struct {
	Process ProcessOptions
	Backoff Backoff
	// Ephemeral marks a process that is expected to exit cleanly once its work
	// is done. A clean exit then ends supervision instead of restarting.
	Ephemeral bool
	// EphemeralDone decides whether a clean exit really means the work is
	// finished. Nil treats every clean exit as finished.
	//
	// It exists because an exit code is not enough to tell the difference.
	// Killing the GitHub runner's listener makes its run.sh exit 0, so a
	// crash and a completed job look identical from here. The caller knows
	// better — the agent asks the job hooks whether a job actually ran — and
	// this is how it says so.
	EphemeralDone func() bool
	// StopTimeout is how long a graceful stop is given before the process is
	// killed. Zero uses the package default.
	StopTimeout time.Duration
	// Start launches the process. Nil uses StartExec.
	Start StartFunc
	// OnEvent receives every state change. It must not block for long.
	OnEvent func(Event)
}

// Supervisor runs a process and restarts it when it fails.
type Supervisor struct {
	opts Options

	mu sync.Mutex
	// current is the running process, so Restart can reach it.
	current Process
	// restarting marks an exit as one the operator asked for, so it does not
	// count against the backoff policy.
	restarting bool
}

// New returns a Supervisor. It validates the options so a misconfiguration
// surfaces before anything is launched.
func New(opts Options) (*Supervisor, error) {
	if opts.Process.Command == "" {
		return nil, errors.New("supervisor: no command to run")
	}
	if opts.Start == nil {
		opts.Start = StartExec
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = stopTimeout
	}
	if opts.Backoff.Factor == 0 && opts.Backoff.Initial == 0 {
		opts.Backoff = DefaultBackoff()
	}
	return &Supervisor{opts: opts}, nil
}

// Run supervises the process until the context is canceled, the process
// completes (when Ephemeral), or the backoff policy gives up.
//
// Canceling the context is a clean shutdown, not an error: Run stops the
// child and returns nil.
func (s *Supervisor) Run(ctx context.Context) error {
	attempt := 0

	for {
		s.emit(Event{Kind: EventStarting, Attempt: attempt})

		proc, err := s.opts.Start(s.opts.Process)
		if err != nil {
			// A process that will not launch is a failure like any other, so
			// it goes through the same backoff rather than returning at once.
			// A missing binary and a crashing one look the same to an
			// operator, and both deserve a retry in case it is transient.
			s.emit(Event{Kind: EventExited, Attempt: attempt, Err: err, ExitCode: -1})

			attempt++
			stop, waitErr := s.waitBeforeRetry(ctx, attempt)
			if stop {
				return waitErr
			}
			continue
		}

		s.setCurrent(proc)
		s.emit(Event{Kind: EventStarted, Attempt: attempt, PID: proc.PID()})
		startedAt := time.Now()

		exitErr, shutdown := s.awaitExit(ctx, proc)
		uptime := time.Since(startedAt)
		asked := s.takeRestartFlag()
		s.setCurrent(nil)

		if shutdown {
			s.emit(Event{Kind: EventStopped, PID: proc.PID(), Uptime: uptime})
			return nil
		}

		s.emit(Event{
			Kind:     EventExited,
			Attempt:  attempt,
			PID:      proc.PID(),
			ExitCode: ExitCode(exitErr),
			Uptime:   uptime,
			Err:      exitErr,
		})

		if asked {
			// An operator asked for this, so it is not a failure: start
			// again immediately and leave the failure history alone.
			s.emit(Event{Kind: EventRestarting, Attempt: attempt, Delay: 0})
			continue
		}

		if s.opts.Ephemeral && exitErr == nil && s.ephemeralFinished() {
			// The work is done. This is the whole point of an ephemeral
			// runner, so it is success, not something to restart.
			s.emit(Event{Kind: EventCompleted, Uptime: uptime})
			return nil
		}

		// A process that stayed up long enough has earned a clean slate.
		if s.opts.Backoff.ResetAfter > 0 && uptime >= s.opts.Backoff.ResetAfter {
			if attempt > 0 {
				s.emit(Event{Kind: EventHealthy, Uptime: uptime})
			}
			attempt = 0
		}

		attempt++
		stop, waitErr := s.waitBeforeRetry(ctx, attempt)
		if stop {
			return waitErr
		}
	}
}

// waitBeforeRetry applies the backoff. It reports whether Run should return,
// and with what error.
func (s *Supervisor) waitBeforeRetry(ctx context.Context, attempt int) (stop bool, err error) {
	if s.opts.Backoff.MaxRestarts > 0 && attempt > s.opts.Backoff.MaxRestarts {
		s.emit(Event{Kind: EventGaveUp, Attempt: attempt - 1})
		return true, fmt.Errorf("%w: gave up after %d restarts", ErrGaveUp, attempt-1)
	}

	delay := s.opts.Backoff.Delay(attempt)
	s.emit(Event{Kind: EventRestarting, Attempt: attempt, Delay: delay})

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return false, nil
	case <-ctx.Done():
		s.emit(Event{Kind: EventStopped})
		return true, nil
	}
}

// awaitExit blocks until the process exits or the context is canceled. On
// cancellation it stops the process and reports shutdown.
func (s *Supervisor) awaitExit(ctx context.Context, proc Process) (exitErr error, shutdown bool) {
	waited := make(chan error, 1)
	go func() { waited <- proc.Wait() }()

	select {
	case err := <-waited:
		return err, false

	case <-ctx.Done():
		s.emit(Event{Kind: EventStopping, PID: proc.PID()})
		if err := proc.Stop(); err != nil {
			// Stop can fail because the process already exited, which is the
			// outcome we wanted anyway.
			s.emit(Event{Kind: EventStopping, PID: proc.PID(), Err: err})
		}

		timer := time.NewTimer(s.opts.StopTimeout)
		defer timer.Stop()

		select {
		case <-waited:
			return nil, true
		case <-timer.C:
			s.emit(Event{Kind: EventKilled, PID: proc.PID(), Delay: s.opts.StopTimeout})
			_ = proc.Kill()
			<-waited
			return nil, true
		}
	}
}

// Restart stops the running process so the supervisor starts it again.
//
// It is not counted as a failure: an operator asking for a restart should not
// push the runner closer to the point where the supervisor gives up, and
// should not have to wait out a backoff delay.
//
// It returns false when nothing is running, which is not an error: a runner
// waiting out a backoff delay is about to start anyway.
func (s *Supervisor) Restart() bool {
	s.mu.Lock()
	proc := s.current
	if proc == nil {
		s.mu.Unlock()
		return false
	}
	s.restarting = true
	s.mu.Unlock()

	// Stop, not Kill: the runner should finish the job it is on, which is
	// the same courtesy a shutdown gets.
	if err := proc.Stop(); err != nil {
		s.emit(Event{Kind: EventStopping, PID: proc.PID(), Err: err})
	}
	return true
}

// ephemeralFinished asks the caller whether a clean exit was really the end
// of the work.
func (s *Supervisor) ephemeralFinished() bool {
	if s.opts.EphemeralDone == nil {
		return true
	}
	return s.opts.EphemeralDone()
}

func (s *Supervisor) setCurrent(proc Process) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = proc
	if proc != nil {
		// A restart requested while nothing was running would otherwise
		// linger and swallow the next real failure.
		s.restarting = false
	}
}

// takeRestartFlag reports whether the exit was an operator's doing, clearing
// it so it applies to exactly one exit.
func (s *Supervisor) takeRestartFlag() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	asked := s.restarting
	s.restarting = false
	return asked
}

func (s *Supervisor) emit(e Event) {
	if s.opts.OnEvent != nil {
		s.opts.OnEvent(e)
	}
}

// Discard is an io.Writer that throws output away, for callers that do not
// want the child's stdout.
var Discard io.Writer = io.Discard
