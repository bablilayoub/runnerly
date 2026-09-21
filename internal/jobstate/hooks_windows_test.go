//go:build windows

package jobstate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBatchHookRunsUnderCmd is the test that could not be written
// anywhere else: it hands the generated hook to the real cmd.exe and
// checks what arrives on the other side.
//
// Quoting is the whole risk here. A path with a space, or with a percent
// sign, is legal on Windows and is exactly what an unquoted or
// half-quoted batch line turns into two arguments, or into a path with a
// chunk replaced by an environment variable. Reading the file back only
// proves what was written; running it proves what it means.
func TestBatchHookRunsUnderCmd(t *testing.T) {
	// A directory whose name needs both defenses at once.
	dir := filepath.Join(t.TempDir(), "runner 100%dir")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Stand in for the runnerly binary: a .cmd that writes down every
	// argument it was given, one per line.
	record := filepath.Join(dir, "args.txt")
	fake := filepath.Join(dir, "fake runnerly.cmd")
	// The record path has a percent sign in it too, so this script has to
	// escape it the same way the hook does. Getting that wrong here is
	// how the first run of this test reported a hook that never ran.
	script := "@echo off\r\n" +
		":loop\r\n" +
		"if \"%~1\"==\"\" goto done\r\n" +
		"echo %~1>>" + batchQuote(record) + "\r\n" +
		"shift\r\n" +
		"goto loop\r\n" +
		":done\r\n" +
		"exit /b 0\r\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	hooks, err := installHooks("windows", dir, fake, "")
	if err != nil {
		t.Fatalf("installHooks: %v", err)
	}

	out, err := exec.CommandContext(t.Context(), "cmd", "/c", hooks.Started).CombinedOutput()
	if err != nil {
		t.Fatalf("running the hook: %v\n%s", err, out)
	}

	body, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the hook never reached the binary it names: %v\n%s", err, out)
	}
	args := strings.Fields(strings.ReplaceAll(string(body), "\r\n", "\n"))

	// The state path is the one that carries the awkward directory name,
	// and it has to arrive as a single argument, spelled exactly.
	want := Path(dir)
	var found bool
	for _, arg := range args {
		if arg == want {
			found = true
		}
	}
	if !found {
		t.Errorf("the state path did not survive the batch file.\nwant %q\ngot  %q", want, args)
	}

	// And the command itself has to be the one Runnerly meant.
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "agent hook started --state") {
		t.Errorf("the hook did not invoke the right command: %q", joined)
	}
}

// A hook that exits non-zero fails the job. Runnerly's bookkeeping going
// wrong must never do that, so the batch file swallows it.
//
// This is the test that found the `call`. Without it, batch hands control
// to the other file and never takes it back, the exit line is never
// reached, and the hook returns the failure it was supposed to absorb.
func TestBatchHookExitsZeroEvenWhenTheBinaryFails(t *testing.T) {
	dir := t.TempDir()

	failing := filepath.Join(dir, "failing.cmd")
	if err := os.WriteFile(failing, []byte("@echo off\r\nexit /b 3\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	hooks, err := installHooks("windows", dir, failing, "")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "cmd", "/c", hooks.Completed)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the hook passed a failure on to the runner: %v\n%s", err, out)
	}
}
