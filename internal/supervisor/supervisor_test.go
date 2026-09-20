package supervisor

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProcess is a process the test drives directly.
type fakeProcess struct {
	pid int

	mu     sync.Mutex
	stops  int
	kills  int
	exited bool

	exitCh chan error
	// onStop runs when Stop is called. A process that honors SIGTERM exits;
	// one that ignores it does not, which is how the kill path is tested.
	onStop func(*fakeProcess)
}

func newFakeProcess(pid int) *fakeProcess {
	return &fakeProcess{pid: pid, exitCh: make(chan error, 1)}
}

// exitsWith pre-programs an exit, so Wait returns at once.
func (f *fakeProcess) exitsWith(err error) *fakeProcess {
	f.exitCh <- err
	return f
}

// honorsStop makes the process exit when asked to stop.
func (f *fakeProcess) honorsStop() *fakeProcess {
	f.onStop = func(p *fakeProcess) { p.finish(nil) }
	return f
}

func (f *fakeProcess) finish(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.exited {
		return
	}
	f.exited = true
	f.exitCh <- err
}

func (f *fakeProcess) Wait() error { return <-f.exitCh }
func (f *fakeProcess) PID() int    { return f.pid }

func (f *fakeProcess) Stop() error {
	f.mu.Lock()
	f.stops++
	f.mu.Unlock()
	if f.onStop != nil {
		f.onStop(f)
	}
	return nil
}

func (f *fakeProcess) Kill() error {
	f.mu.Lock()
	f.kills++
	f.mu.Unlock()
	f.finish(errors.New("killed"))
	return nil
}

func (f *fakeProcess) counts() (stops, kills int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops, f.kills
}

// recorder collects events for assertions.
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) kinds() []EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EventKind, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.Kind)
	}
	return out
}

func (r *recorder) count(kind EventKind) int {
	n := 0
	for _, k := range r.kinds() {
		if k == kind {
			n++
		}
	}
	return n
}

func (r *recorder) first(kind EventKind) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Kind == kind {
			return e, true
		}
	}
	return Event{}, false
}

// fastBackoff keeps tests quick while preserving the shape of the policy.
// maxRestarts counts restarts, so the process is started maxRestarts+1 times
// before the supervisor gives up.
func fastBackoff(maxRestarts int) Backoff {
	return Backoff{
		Initial:     time.Millisecond,
		Max:         5 * time.Millisecond,
		Factor:      2,
		MaxRestarts: maxRestarts,
		ResetAfter:  time.Hour, // never forgiven unless a test says otherwise
	}
}

func TestBackoffFollowsThePlannedSchedule(t *testing.T) {
	b := DefaultBackoff()
	want := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		80 * time.Second,
	}
	for i, w := range want {
		if got := b.Delay(i + 1); got != w {
			t.Errorf("Delay(%d) = %v, want %v", i+1, got, w)
		}
	}
	// Beyond the schedule the delay is capped, not unbounded.
	if got := b.Delay(10); got != b.Max {
		t.Errorf("Delay(10) = %v, want the cap %v", got, b.Max)
	}
}

func TestBackoffEdgeCases(t *testing.T) {
	b := DefaultBackoff()
	if got := b.Delay(0); got != b.Initial {
		t.Errorf("Delay(0) = %v, want it treated as the first attempt", got)
	}
	if got := (Backoff{}).Delay(3); got != 0 {
		t.Errorf("a zero policy returned %v, want no delay", got)
	}
	// A factor below 1 would shrink the delay; it is clamped instead.
	shrinking := Backoff{Initial: time.Second, Factor: 0.5, Max: time.Minute}
	if got := shrinking.Delay(3); got != time.Second {
		t.Errorf("Delay(3) = %v, want the delay never to shrink", got)
	}
}

func TestNewRejectsAnEmptyCommand(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("New() accepted options with no command")
	}
}

func TestRestartsAFailingProcess(t *testing.T) {
	var rec recorder
	starts := make(chan int, 10)
	attempts := 0

	s, err := New(Options{
		Process: ProcessOptions{Command: "./run.sh"},
		Backoff: fastBackoff(0), // never give up
		OnEvent: rec.add,
		Start: func(ProcessOptions) (Process, error) {
			attempts++
			starts <- attempts
			return newFakeProcess(attempts).exitsWith(errors.New("crashed")), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Wait for the fourth launch, which proves the first three all exited and
	// were restarted. Canceling on the third would race its exit.
	for i := 1; i <= 4; i++ {
		select {
		case <-starts:
		case <-time.After(2 * time.Second):
			t.Fatalf("process was not restarted %d times", i)
		}
	}
	cancel()

	if err := <-done; err != nil {
		t.Errorf("Run() = %v, want nil after cancellation", err)
	}
	if rec.count(EventExited) < 3 {
		t.Errorf("saw %d exits, want at least 3: %v", rec.count(EventExited), rec.kinds())
	}
	if rec.count(EventRestarting) < 3 {
		t.Errorf("saw %d restarts, want at least 3", rec.count(EventRestarting))
	}
}

func TestGivesUpAfterTooManyFailures(t *testing.T) {
	var rec recorder
	starts := 0

	s, err := New(Options{
		Process: ProcessOptions{Command: "./run.sh"},
		Backoff: fastBackoff(3),
		OnEvent: rec.add,
		Start: func(ProcessOptions) (Process, error) {
			starts++
			return newFakeProcess(starts).exitsWith(errors.New("crashed")), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	runErr := s.Run(context.Background())
	if !errors.Is(runErr, ErrGaveUp) {
		t.Fatalf("Run() = %v, want ErrGaveUp", runErr)
	}
	// One launch plus three restarts.
	if starts != 4 {
		t.Errorf("started %d times, want 4", starts)
	}
	if rec.count(EventGaveUp) != 1 {
		t.Errorf("gave_up emitted %d times, want 1: %v", rec.count(EventGaveUp), rec.kinds())
	}
}

func TestAProcessThatStaysUpClearsItsFailureHistory(t *testing.T) {
	var rec recorder
	var starts atomic.Int32

	// Only the long-lived process clears the history: the crashing ones exit
	// in well under this.
	const healthyAfter = 50 * time.Millisecond

	backoff := fastBackoff(2)
	backoff.ResetAfter = healthyAfter

	s, err := New(Options{
		Process: ProcessOptions{Command: "./run.sh"},
		Backoff: backoff,
		OnEvent: rec.add,
		Start: func(ProcessOptions) (Process, error) {
			n := int(starts.Add(1))
			if n != 2 {
				return newFakeProcess(n).exitsWith(errors.New("crashed")), nil
			}
			// The second process stays up long enough to be forgiven, then
			// fails like the others.
			p := newFakeProcess(n).honorsStop()
			go func() {
				time.Sleep(2 * healthyAfter)
				p.finish(errors.New("crashed later"))
			}()
			return p, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	runErr := s.Run(context.Background())
	if !errors.Is(runErr, ErrGaveUp) {
		t.Fatalf("Run() = %v, want ErrGaveUp once the restarts really are exhausted", runErr)
	}

	if rec.count(EventHealthy) == 0 {
		t.Errorf("no healthy event after a long-lived process: %v", rec.kinds())
	}
	// Two restarts are allowed, so without the reset the run would end after
	// 3 starts. Clearing the history mid-run buys exactly one more.
	if got := int(starts.Load()); got != 4 {
		t.Errorf("started %d times, want 4: the reset should grant another restart", got)
	}
}

func TestEphemeralProcessEndsOnACleanExit(t *testing.T) {
	var rec recorder
	starts := 0

	s, err := New(Options{
		Process:   ProcessOptions{Command: "./run.sh"},
		Backoff:   fastBackoff(5),
		Ephemeral: true,
		OnEvent:   rec.add,
		Start: func(ProcessOptions) (Process, error) {
			starts++
			return newFakeProcess(starts).exitsWith(nil), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v, want nil: a clean exit is the point of an ephemeral runner", err)
	}
	if starts != 1 {
		t.Errorf("started %d times, want 1", starts)
	}
	if rec.count(EventRestarting) != 0 {
		t.Errorf("an ephemeral runner was restarted after finishing: %v", rec.kinds())
	}
}

func TestEphemeralProcessStillRestartsAfterACrash(t *testing.T) {
	starts := 0
	s, err := New(Options{
		Process:   ProcessOptions{Command: "./run.sh"},
		Backoff:   fastBackoff(2),
		Ephemeral: true,
		Start: func(ProcessOptions) (Process, error) {
			starts++
			return newFakeProcess(starts).exitsWith(errors.New("crashed")), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Run(context.Background()); !errors.Is(err, ErrGaveUp) {
		t.Fatalf("Run() = %v, want ErrGaveUp", err)
	}
	if starts != 3 {
		t.Errorf("started %d times, want 3: a crash is not a completed job", starts)
	}
}

func TestCancellationStopsTheProcessGracefully(t *testing.T) {
	var rec recorder
	proc := newFakeProcess(42).honorsStop()
	started := make(chan struct{})

	s, err := New(Options{
		Process: ProcessOptions{Command: "./run.sh"},
		Backoff: fastBackoff(0),
		OnEvent: rec.add,
		Start: func(ProcessOptions) (Process, error) {
			close(started)
			return proc, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	<-started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want nil: cancellation is a clean shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}

	stops, kills := proc.counts()
	if stops != 1 {
		t.Errorf("Stop called %d times, want 1", stops)
	}
	if kills != 0 {
		t.Errorf("Kill called %d times, want 0 for a process that honors Stop", kills)
	}
	if rec.count(EventStopped) == 0 {
		t.Errorf("no stopped event: %v", rec.kinds())
	}
}

func TestAProcessThatIgnoresStopIsKilled(t *testing.T) {
	var rec recorder
	proc := newFakeProcess(42) // no onStop: it ignores SIGTERM
	started := make(chan struct{})

	s, err := New(Options{
		Process:     ProcessOptions{Command: "./run.sh"},
		Backoff:     fastBackoff(0),
		StopTimeout: 10 * time.Millisecond,
		OnEvent:     rec.add,
		Start: func(ProcessOptions) (Process, error) {
			close(started)
			return proc, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	<-started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return; the kill path did not fire")
	}

	_, kills := proc.counts()
	if kills != 1 {
		t.Errorf("Kill called %d times, want 1", kills)
	}
	if rec.count(EventKilled) != 1 {
		t.Errorf("no killed event: %v", rec.kinds())
	}
}

func TestAProcessThatWillNotLaunchIsRetried(t *testing.T) {
	var rec recorder
	starts := 0

	s, err := New(Options{
		Process: ProcessOptions{Command: "./missing.sh"},
		Backoff: fastBackoff(3),
		OnEvent: rec.add,
		Start: func(ProcessOptions) (Process, error) {
			starts++
			return nil, errors.New("no such file or directory")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if runErr := s.Run(context.Background()); !errors.Is(runErr, ErrGaveUp) {
		t.Fatalf("Run() = %v, want ErrGaveUp", runErr)
	}
	if starts != 4 {
		t.Errorf("tried %d times, want 4: a launch failure may be transient", starts)
	}

	exited, ok := rec.first(EventExited)
	if !ok {
		t.Fatalf("no exited event for a launch failure: %v", rec.kinds())
	}
	if exited.Err == nil {
		t.Error("the launch failure was not reported on the event")
	}
}

func TestExitCode(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", got)
	}
	if got := ExitCode(errors.New("signaled")); got != -1 {
		t.Errorf("ExitCode(non-exit error) = %d, want -1", got)
	}

	// A real non-zero exit, so the *exec.ExitError path is exercised.
	err := exec.CommandContext(context.Background(), "sh", "-c", "exit 3").Run()
	if got := ExitCode(err); got != 3 {
		t.Errorf("ExitCode() = %d, want 3", got)
	}
}
