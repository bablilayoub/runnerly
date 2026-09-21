//go:build windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchtasksImportsTheGeneratedTask hands the generated XML to the real
// schtasks.
//
// Task Scheduler validates the whole document on import and rejects it
// with "the task XML contains a value which is incorrectly formatted or
// out of range" — one message for a misspelled element, an element in the
// wrong order, a duration that is not ISO 8601, and the wrong text
// encoding. Reading the file cannot tell those apart. Importing it can.
func TestSchtasksImportsTheGeneratedTask(t *testing.T) {
	if _, err := exec.LookPath("schtasks"); err != nil {
		t.Skip("schtasks is not on PATH")
	}

	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")
	xmlPath := filepath.Join(t.TempDir(), "task.xml")

	if _, _, err := runCLI(t, nil, "--config", cfg,
		"agent", "schtasks", "runnerly-01", "--output", xmlPath); err != nil {
		t.Fatalf("agent schtasks: %v", err)
	}

	// A name of its own, deleted whatever happens, so a failed run does
	// not leave a task registered on the machine.
	const name = `\RunnerlyTest\import-check`
	t.Cleanup(func() {
		_ = exec.CommandContext(t.Context(), "schtasks", "/delete", "/tn", name, "/f").Run()
	})

	// SYSTEM because it needs no password. What is being checked is
	// whether Task Scheduler accepts the document, not which account it
	// would run as.
	create := exec.CommandContext(t.Context(), "schtasks",
		"/create", "/xml", xmlPath, "/tn", name, "/ru", "SYSTEM", "/f")
	if out, err := create.CombinedOutput(); err != nil {
		if strings.Contains(string(out), "Access is denied") {
			t.Skipf("registering a task needs an administrator: %s", out)
		}
		body, _ := os.ReadFile(xmlPath) //nolint:errcheck // for the failure message only
		t.Fatalf("Task Scheduler rejected the generated task: %v\n%s\n---\n%s", err, out, body)
	}

	query := exec.CommandContext(t.Context(), "schtasks", "/query", "/tn", name, "/v", "/fo", "list")
	out, err := query.CombinedOutput()
	if err != nil {
		t.Fatalf("querying the task: %v\n%s", err, out)
	}

	text := string(out)
	for _, want := range []string{"runnerly-agent", "--name runnerly-01"} {
		if !strings.Contains(text, want) {
			t.Errorf("the registered task does not mention %q:\n%s", want, text)
		}
	}
	// The default execution time limit is three days, after which Windows
	// stops the task — which for a runner is a machine that quietly stops
	// taking jobs every three days.
	if !strings.Contains(text, "Disabled") && !strings.Contains(text, "72:00:00") {
		t.Logf("execution time limit as registered:\n%s", text)
	}
	if strings.Contains(text, "72:00:00") {
		t.Error("the task kept the three-day execution limit")
	}
}
