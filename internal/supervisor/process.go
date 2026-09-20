package supervisor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Process is a running child process.
//
// It is an interface so the supervisor's restart logic can be tested without
// spawning anything: the loop only ever sees Wait, Stop and Kill.
type Process interface {
	// Wait blocks until the process exits and returns its exit error, or nil
	// if it exited cleanly.
	Wait() error
	// Stop asks the process to terminate gracefully. For the official runner
	// this is SIGTERM, which lets it finish the job it is running.
	Stop() error
	// Kill terminates the process immediately.
	Kill() error
	// PID identifies the process in logs.
	PID() int
}

// ProcessOptions describes a process to launch.
type ProcessOptions struct {
	Dir     string
	Command string
	Args    []string
	// Env is the full environment. Nil inherits the parent's.
	Env []string
	// Stdout and Stderr receive the child's output. Nil discards it.
	Stdout io.Writer
	Stderr io.Writer
}

// StartFunc launches a process. The supervisor takes one so tests can supply
// a fake.
type StartFunc func(opts ProcessOptions) (Process, error)

// execProcess is a real child process.
type execProcess struct {
	cmd *exec.Cmd

	// waitOnce guards cmd.Wait, which must be called exactly once, while
	// letting callers Wait from wherever they like.
	waitOnce sync.Once
	waitErr  error
	done     chan struct{}
}

// StartExec launches a real process. On Unix the child gets its own process
// group, so stopping it also stops whatever it spawned — the official runner's
// run.sh starts Runner.Listener, and signaling only the script would orphan
// the listener.
func StartExec(opts ProcessOptions) (Process, error) {
	if opts.Command == "" {
		return nil, errors.New("no command to run")
	}

	// exec.Command, not CommandContext, on purpose: tying the process to a
	// context would have cancellation SIGKILL it, and the whole point of Stop
	// is to send SIGTERM first so the runner can finish the job it is on.
	// Lifetime is managed explicitly through Stop and Kill instead.
	//
	// #nosec G204 -- the command is the runner script in a directory Runnerly installed
	cmd := exec.Command(opts.Command, opts.Args...) //nolint:noctx // see above
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s in %s: %w", opts.Command, opts.Dir, err)
	}

	return &execProcess{cmd: cmd, done: make(chan struct{})}, nil
}

func (p *execProcess) Wait() error {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		close(p.done)
	})
	<-p.done
	return p.waitErr
}

func (p *execProcess) Stop() error { return p.signal(os.Interrupt, terminateSignal()) }

func (p *execProcess) Kill() error { return p.signal(os.Kill, os.Kill) }

// signal delivers to the whole process group where the platform supports it,
// falling back to the process itself.
func (p *execProcess) signal(fallback, sig os.Signal) error {
	if p.cmd.Process == nil {
		return errors.New("process was never started")
	}
	if err := signalGroup(p.cmd.Process.Pid, sig); err == nil {
		return nil
	}
	return p.cmd.Process.Signal(fallback)
}

func (p *execProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// ExitCode extracts a process exit code from a Wait error. It returns 0 for a
// clean exit and -1 when the process was signaled or the code is unknown.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// stopTimeout is how long Stop is given before Kill. The official runner
// finishes its current job on SIGTERM, and a job can take a while, so this is
// generous by default and configurable.
const stopTimeout = 30 * time.Second
