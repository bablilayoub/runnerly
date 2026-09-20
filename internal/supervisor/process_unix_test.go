//go:build unix

package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// writeScript creates an executable shell script and returns its path.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// readPIDs returns the process ids a script has appended to its log.
func readPIDs(t *testing.T, path string) []int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Fields(string(data)) {
		if pid, convErr := strconv.Atoi(line); convErr == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// waitForPIDs blocks until the log holds at least n process ids.
func waitForPIDs(t *testing.T, path string, n int, why string) []int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pids := readPIDs(t, path); len(pids) >= n {
			return pids
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d process ids (%s); got %v", n, why, readPIDs(t, path))
	return nil
}

// alive reports whether a process exists. Signal 0 performs the permission
// and existence checks without delivering anything.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func waitUntilGone(t *testing.T, pid int, why string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("process %d is still running (%s)", pid, why)
}

func TestStartExecRunsAndStopsARealProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pids")
	// exec replaces the shell, so the recorded pid is the one that sleeps and
	// stopping it really stops the sleep.
	script := writeScript(t, dir, "run.sh", `echo $$ >> "$1"`+"\n"+`exec sleep 30`+"\n")

	proc, err := StartExec(ProcessOptions{Dir: dir, Command: script, Args: []string{pidFile}})
	if err != nil {
		t.Fatalf("StartExec() error = %v", err)
	}

	pids := waitForPIDs(t, pidFile, 1, "the script to start")
	if !alive(pids[0]) {
		t.Fatalf("process %d is not running", pids[0])
	}
	if proc.PID() != pids[0] {
		t.Errorf("PID() = %d, want %d", proc.PID(), pids[0])
	}

	waited := make(chan error, 1)
	go func() { waited <- proc.Wait() }()

	select {
	case <-waited:
		t.Fatal("Wait() returned while the process was still sleeping")
	case <-time.After(100 * time.Millisecond):
	}

	if err := proc.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("the process did not exit after Stop()")
	}
	waitUntilGone(t, pids[0], "after Stop")
}

func TestStopReachesTheWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	childFile := filepath.Join(dir, "child")
	// The official runner's run.sh starts Runner.Listener the same way:
	// signaling only the script would leave the child running.
	script := writeScript(t, dir, "run.sh", strings.Join([]string{
		`sleep 30 &`,
		`echo $! >> "$1"`,
		`wait`,
	}, "\n")+"\n")

	proc, err := StartExec(ProcessOptions{Dir: dir, Command: script, Args: []string{childFile}})
	if err != nil {
		t.Fatalf("StartExec() error = %v", err)
	}

	child := waitForPIDs(t, childFile, 1, "the child to start")[0]
	if !alive(child) {
		t.Fatalf("child %d is not running", child)
	}

	waited := make(chan error, 1)
	go func() { waited <- proc.Wait() }()

	if err := proc.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatal("the script did not exit after Stop()")
	}

	waitUntilGone(t, child, "the child should die with its process group")
}

// TestSupervisorRecoversFromAKilledProcess is Milestone 3's acceptance
// criterion: kill the runner process and the supervisor brings it back.
func TestSupervisorRecoversFromAKilledProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pids")
	script := writeScript(t, dir, "run.sh", `echo $$ >> "$1"`+"\n"+`exec sleep 30`+"\n")

	var restarts atomic.Int32
	backoff := DefaultBackoff()
	backoff.Initial = 10 * time.Millisecond
	backoff.Max = 50 * time.Millisecond

	s, err := New(Options{
		Process: ProcessOptions{Dir: dir, Command: script, Args: []string{pidFile}},
		Backoff: backoff,
		OnEvent: func(e Event) {
			if e.Kind == EventRestarting {
				restarts.Add(1)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	first := waitForPIDs(t, pidFile, 1, "the first run")[0]

	// Kill it the way a crash would, from outside the supervisor.
	if err := syscall.Kill(first, syscall.SIGKILL); err != nil {
		t.Fatalf("kill %d: %v", first, err)
	}

	pids := waitForPIDs(t, pidFile, 2, "the supervisor to restart the process")
	second := pids[1]
	if second == first {
		t.Fatalf("the same process id came back: %d", second)
	}
	if !alive(second) {
		t.Fatalf("the replacement process %d is not running", second)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want nil after cancellation", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}

	waitUntilGone(t, second, "shutdown should stop the replacement too")
	if restarts.Load() == 0 {
		t.Error("no restart was reported")
	}
}

func TestSupervisorReportsARealExitCode(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "run.sh", "exit 7\n")

	var codes []int
	s, err := New(Options{
		Process: ProcessOptions{Dir: dir, Command: script},
		Backoff: Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 1},
		OnEvent: func(e Event) {
			if e.Kind == EventExited {
				codes = append(codes, e.ExitCode)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if runErr := s.Run(context.Background()); !errors.Is(runErr, ErrGaveUp) {
		t.Fatalf("Run() = %v, want ErrGaveUp", runErr)
	}
	for _, c := range codes {
		if c != 7 {
			t.Errorf("exit code = %d, want 7", c)
		}
	}
	if len(codes) != 2 {
		t.Errorf("recorded %d exits, want 2 (one launch plus one restart)", len(codes))
	}
}

func TestStartExecRejectsAMissingCommand(t *testing.T) {
	if _, err := StartExec(ProcessOptions{Dir: t.TempDir(), Command: ""}); err == nil {
		t.Error("StartExec() accepted an empty command")
	}
	if _, err := StartExec(ProcessOptions{Dir: t.TempDir(), Command: "./does-not-exist.sh"}); err == nil {
		t.Error("StartExec() accepted a command that does not exist")
	}
}
