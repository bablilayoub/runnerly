//go:build windows

package jobstate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// powershell returns the shell GitHub's runner would use, preferring the
// one it prefers.
func powershell(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"pwsh", "powershell"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("neither pwsh nor powershell is on PATH")
	return ""
}

// buildRecorder compiles a real .exe that writes down the arguments it
// was given, one per line, and exits with RUNNERLY_EXIT.
//
// A real executable rather than a script, because PowerShell passes
// arguments to a native program differently from how it passes them to a
// .ps1 — and a native program is what the hook actually calls.
func buildRecorder(t *testing.T, dir string) string {
	t.Helper()

	src := filepath.Join(dir, "recorder")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module recorder\n\ngo 1.25\n")
	write("main.go", `package main

import (
	"os"
	"strconv"
	"strings"
)

func main() {
	if record := os.Getenv("RUNNERLY_RECORD"); record != "" {
		_ = os.WriteFile(record, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	code, _ := strconv.Atoi(os.Getenv("RUNNERLY_EXIT"))
	os.Exit(code)
}
`)

	exe := filepath.Join(dir, "recorder.exe")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", exe, ".")
	build.Dir = src
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the recorder: %v\n%s", err, out)
	}
	return exe
}

// TestPowerShellHookRunsTheWayTheRunnerRunsIt invokes the hook exactly as
// GitHub's runner does — `pwsh -command ". '<path>'"` — and reads back
// what reached the other side.
//
// Quoting is the risk. A path with a space has to arrive as one argument,
// and a path with an apostrophe has to arrive at all: a single quote is
// the one character that ends a PowerShell single-quoted string early.
// Reading the generated file proves what was written; running it proves
// what it means.
func TestPowerShellHookRunsTheWayTheRunnerRunsIt(t *testing.T) {
	shell := powershell(t)
	root := t.TempDir()

	// The hook's own path stays plain: the runner wraps it in single
	// quotes itself, so an apostrophe there would break the runner rather
	// than Runnerly. The awkward characters go in an argument, which is
	// what this quoting is responsible for.
	dir := filepath.Join(root, "runner dir")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "O'Brien 100%conf", "config.yaml")
	record := filepath.Join(root, "args.txt")

	hooks, err := installHooks("windows", dir, buildRecorder(t, root), configPath)
	if err != nil {
		t.Fatalf("installHooks: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), shell, "-command", ". '"+hooks.Started+"'")
	cmd.Env = append(os.Environ(), "RUNNERLY_RECORD="+record, "RUNNERLY_EXIT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running the hook: %v\n%s", err, out)
	}

	body, err := os.ReadFile(record) //nolint:gosec // a path this test made
	if err != nil {
		t.Fatalf("the hook never reached the binary it names: %v", err)
	}
	// One argument per line, not per word: the whole point is that a path
	// with a space arrives as one argument, and splitting on whitespace
	// would hide exactly the failure being looked for.
	var args []string
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			args = append(args, trimmed)
		}
	}

	for _, want := range []string{"agent", "hook", "started", Path(dir), configPath} {
		var found bool
		for _, arg := range args {
			if arg == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q did not survive the hook.\ngot %q", want, args)
		}
	}
}

// A hook that exits non-zero fails the job. Runnerly's bookkeeping going
// wrong must never do that, so the hook swallows it.
func TestPowerShellHookExitsZeroEvenWhenTheBinaryFails(t *testing.T) {
	shell := powershell(t)
	root := t.TempDir()

	hooks, err := installHooks("windows", root, buildRecorder(t, root), "")
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), shell, "-command", ". '"+hooks.Completed+"'")
	cmd.Env = append(os.Environ(), "RUNNERLY_EXIT=3")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the hook passed a failure on to the runner: %v\n%s", err, out)
	}
}
