//go:build windows

package supervisor

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"testing"
	"time"
)

// TestGracefulStopDeliversCtrlBreak is the point of the Windows CI job.
//
// There are no signals here, so a graceful stop is a console control
// event, and there is no way to check by reading the code that the event
// reaches the child rather than the supervisor, or reaches anything at
// all. Getting it wrong does not look like a failure — the stop quietly
// does nothing, the timeout expires, and the runner is killed with a job
// half finished.
//
// The child is this test binary re-run as a helper, because it needs to
// install a handler and say what it saw.
func TestGracefulStopDeliversCtrlBreak(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "interrupted")

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), self, "-test.run=TestHelperWaitsForCtrlBreak")
	cmd.Env = append(os.Environ(),
		"RUNNERLY_HELPER_MARKER="+marker,
	)
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	// Wait for the helper to have installed its handler. Sending the event
	// before that races, and a flaky test here is worse than a slow one.
	ready := marker + ".ready"
	waitForFile(t, ready, "the helper to install its handler")

	if err := signalGroup(cmd.Process.Pid, os.Interrupt); err != nil {
		t.Fatalf("signalGroup: %v", err)
	}

	waitForFile(t, marker, "the helper to report the event")

	if err := cmd.Wait(); err != nil {
		t.Errorf("the helper did not exit cleanly after Ctrl+Break: %v", err)
	}
}

// The event must go to the child's group and not to this process. If it
// came here too, stopping a runner would stop the agent supervising it.
func TestGracefulStopDoesNotHitTheSupervisor(t *testing.T) {
	here := make(chan os.Signal, 1)
	signal.Notify(here, os.Interrupt)
	defer signal.Stop(here)

	marker := filepath.Join(t.TempDir(), "interrupted")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), self, "-test.run=TestHelperWaitsForCtrlBreak")
	cmd.Env = append(os.Environ(), "RUNNERLY_HELPER_MARKER="+marker)
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	waitForFile(t, marker+".ready", "the helper to install its handler")
	if err := signalGroup(cmd.Process.Pid, os.Interrupt); err != nil {
		t.Fatalf("signalGroup: %v", err)
	}
	waitForFile(t, marker, "the helper to report the event")

	select {
	case <-here:
		t.Fatal("the supervisor received the event it sent to the runner")
	case <-time.After(500 * time.Millisecond):
	}
}

// TestHelperWaitsForCtrlBreak is not a test. It is the child process the
// two above start: it waits for the console event and writes down that it
// arrived.
func TestHelperWaitsForCtrlBreak(t *testing.T) {
	marker := os.Getenv("RUNNERLY_HELPER_MARKER")
	if marker == "" {
		t.Skip("not running as the helper process")
	}

	events := make(chan os.Signal, 1)
	signal.Notify(events, os.Interrupt)

	if err := os.WriteFile(marker+".ready", []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case <-events:
		if err := os.WriteFile(marker, []byte("interrupted"), 0o600); err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no console event arrived")
	}
}

func waitForFile(t *testing.T, path, why string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", why)
}
