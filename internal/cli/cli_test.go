package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/doctor"
	"github.com/bablilayoub/runnerly/internal/version"
)

// run executes the command tree against buffers and returns what it wrote.
func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRootCommand(&out, &errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func TestVersionShort(t *testing.T) {
	out, _, err := run(t, "version", "--short")
	if err != nil {
		t.Fatalf("version --short: %v", err)
	}
	if strings.TrimSpace(out) != version.Get().Short() {
		t.Errorf("stdout = %q, want %q", out, version.Get().Short())
	}
}

func TestVersionJSON(t *testing.T) {
	out, _, err := run(t, "version", "--json")
	if err != nil {
		t.Fatalf("version --json: %v", err)
	}
	var got version.Info
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Go == "" || got.OS == "" {
		t.Errorf("incomplete version info: %+v", got)
	}
}

func TestRootWithoutArgsPrintsHelp(t *testing.T) {
	out, _, err := run(t)
	if err != nil {
		t.Fatalf("bare invocation returned an error: %v", err)
	}
	if !strings.Contains(out, "runnerly") || !strings.Contains(out, "doctor") {
		t.Errorf("help output looks wrong:\n%s", out)
	}
}

func TestConfigPathHonoursFlag(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom.yaml")
	out, _, err := run(t, "--config", want, "config", "path")
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if strings.TrimSpace(out) != want {
		t.Errorf("stdout = %q, want %q", strings.TrimSpace(out), want)
	}
}

func TestConfigInitWritesThenRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	if _, _, err := run(t, "--config", path, "config", "init"); err != nil {
		t.Fatalf("config init: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config init did not create the file: %v", err)
	}

	_, _, err := run(t, "--config", path, "config", "init")
	if err == nil {
		t.Fatal("second config init overwrote the file without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should point at --force, got: %v", err)
	}

	if _, _, err := run(t, "--config", path, "config", "init", "--force"); err != nil {
		t.Errorf("config init --force: %v", err)
	}
}

func TestConfigShowIncludesDefaultsWhenNoFileExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	out, _, err := run(t, "--config", path, "config", "show")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	for _, want := range []string{"no file at", "host: github.com", "type: docker"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestConfigValidateReportsTheOffendingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("executor:\n  type: podman\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := run(t, "--config", path, "config", "validate")
	if err == nil {
		t.Fatal("config validate accepted an invalid executor")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "executor.type") {
		t.Errorf("error should name the file and the field, got: %v", err)
	}
}

func TestConfigValidateAcceptsAGoodFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, _, err := run(t, "--config", path, "config", "init"); err != nil {
		t.Fatal(err)
	}
	out, _, err := run(t, "--config", path, "config", "validate")
	if err != nil {
		t.Fatalf("config validate: %v", err)
	}
	if !strings.Contains(out, "is valid") {
		t.Errorf("output = %q", out)
	}
}

// TestDoctorOfflineJSON exercises the wiring between the command and the check
// engine. It runs offline and with the host executor so the result does not
// depend on Docker or the network being available on the test machine.
func TestDoctorOfflineJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("executor:\n  type: host\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _, err := run(t, "--config", path, "doctor", "--offline", "--json")
	// A failing check is reported through ExitError; anything else is a bug.
	if err != nil {
		var exit *ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("doctor returned an unexpected error: %v", err)
		}
	}

	var report doctor.Report
	if jsonErr := json.Unmarshal([]byte(out), &report); jsonErr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", jsonErr, out)
	}
	if len(report.Checks) == 0 {
		t.Fatal("report contains no checks")
	}
	for _, name := range []string{"outbound HTTPS", "GitHub API"} {
		for _, c := range report.Checks {
			if c.Name == name && c.Status != doctor.StatusSkip {
				t.Errorf("%s = %q, want skip with --offline", name, c.Status)
			}
		}
	}
}

func TestDoctorExitsNonZeroOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	// An invalid executor makes the configuration check fail deterministically.
	if err := os.WriteFile(path, []byte("executor:\n  type: podman\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := run(t, "--config", path, "doctor", "--offline")
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("doctor returned %v, want an ExitError", err)
	}
	if exit.Code != 1 {
		t.Errorf("exit code = %d, want 1", exit.Code)
	}
}

func TestUnknownCommandIsAnError(t *testing.T) {
	if _, _, err := run(t, "runner", "create"); err == nil {
		t.Fatal("unknown command did not produce an error")
	}
}
