package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/state"
)

// recordInstalled creates a directory that looks like a configured runner and
// records it beside the given config file.
func recordInstalled(t *testing.T, configPath, name string) state.Runner {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".runner", "run.sh"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("#!/bin/sh\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := state.Runner{
		Name:   name,
		Dir:    dir,
		Host:   "github.com",
		Scope:  github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Labels: []string{"self-hosted", "linux", "x64"},
	}
	if err := state.Put(state.Path(configPath), r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAgentStatusWithNothingInstalled(t *testing.T) {
	cfg := configIn(t, "")
	out, _, err := runCLI(t, nil, "--config", cfg, "agent", "status")
	if err != nil {
		t.Fatalf("agent status: %v", err)
	}
	if !strings.Contains(out, "no runner is installed") {
		t.Errorf("output = %q", out)
	}
	if !strings.Contains(out, "runner create") {
		t.Errorf("output should say how to install one:\n%s", out)
	}
}

func TestAgentStatusListsInstalledRunners(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")

	out, _, err := runCLI(t, nil, "--config", cfg, "agent", "status")
	if err != nil {
		t.Fatalf("agent status: %v", err)
	}
	for _, want := range []string{"runnerly-01", "acme/widgets", "yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestAgentStatusFlagsARunnerThatCannotStart(t *testing.T) {
	cfg := configIn(t, "")
	r := recordInstalled(t, cfg, "broken")
	// Remove the marker config.sh writes on a successful registration.
	if err := os.Remove(filepath.Join(r.Dir, ".runner")); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCLI(t, nil, "--config", cfg, "agent", "status")
	if err != nil {
		t.Fatalf("agent status: %v", err)
	}
	if !strings.Contains(out, "no") {
		t.Errorf("output should mark the runner as not ready:\n%s", out)
	}
	if !strings.Contains(out, "no configured runner") {
		t.Errorf("output should explain why:\n%s", out)
	}
}

func TestAgentSystemdRendersAUnit(t *testing.T) {
	cfg := configIn(t, "")
	r := recordInstalled(t, cfg, "runnerly-01")

	out, _, err := runCLI(t, nil, "--config", cfg, "agent", "systemd", "runnerly-01")
	if err != nil {
		t.Fatalf("agent systemd: %v", err)
	}

	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"User=runnerly",
		"Group=runnerly",
		"--name runnerly-01",
		"WorkingDirectory=" + r.Dir,
		"ReadWritePaths=" + r.Dir,
		"KillSignal=SIGTERM",
		"Restart=always",
		"NoNewPrivileges=true",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("unit missing %q:\n%s", want, out)
		}
	}

	// It must never run privileged commands itself, only show them.
	for _, want := range []string{"sudo install", "systemctl enable --now", "journalctl"} {
		if !strings.Contains(out, want) {
			t.Errorf("instructions missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "User=root") {
		t.Errorf("the unit should not default to root:\n%s", out)
	}
}

func TestAgentSystemdHonorsUserAndGroup(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")

	out, _, err := runCLI(t, nil, "--config", cfg,
		"agent", "systemd", "--user", "ci", "--group", "builders")
	if err != nil {
		t.Fatalf("agent systemd: %v", err)
	}
	if !strings.Contains(out, "User=ci") || !strings.Contains(out, "Group=builders") {
		t.Errorf("unit did not honor --user/--group:\n%s", out)
	}
}

func TestAgentSystemdGroupDefaultsToUser(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")

	out, _, err := runCLI(t, nil, "--config", cfg, "agent", "systemd", "--user", "ci")
	if err != nil {
		t.Fatalf("agent systemd: %v", err)
	}
	if !strings.Contains(out, "Group=ci") {
		t.Errorf("group should default to the user:\n%s", out)
	}
}

func TestAgentSystemdWritesToAFile(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")
	target := filepath.Join(t.TempDir(), "runnerly-agent.service")

	out, _, err := runCLI(t, nil, "--config", cfg, "agent", "systemd", "--output", target)
	if err != nil {
		t.Fatalf("agent systemd --output: %v", err)
	}

	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("unit was not written: %v", err)
	}
	if !strings.Contains(string(written), "[Service]") {
		t.Errorf("file does not look like a unit:\n%s", written)
	}
	if !strings.Contains(out, target) {
		t.Errorf("output should name the file it wrote:\n%s", out)
	}
}

func TestAgentCommandsNeedARunnerName(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "one")
	recordInstalled(t, cfg, "two")

	_, _, err := runCLI(t, nil, "--config", cfg, "agent", "systemd")
	if err == nil {
		t.Fatal("agent systemd guessed between two runners")
	}
	for _, want := range []string{"one", "two"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should list the candidates, missing %q: %v", want, err)
		}
	}
}

func TestAgentCommandsRejectAnUnknownRunner(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")

	_, _, err := runCLI(t, nil, "--config", cfg, "agent", "systemd", "absent")
	if err == nil {
		t.Fatal("expected an error for a runner that is not installed")
	}
	if !strings.Contains(err.Error(), "agent status") {
		t.Errorf("the error should suggest listing them, got: %v", err)
	}
}

func TestRunnerRemoveForgetsTheRunnerLocally(t *testing.T) {
	cfg := configIn(t, "")
	r := recordInstalled(t, cfg, "runnerly-01")

	handler := func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"runners":[{"id":7,"name":"runnerly-01","os":"linux","status":"online"}]}`))
	}

	out, _, err := runCLI(t, []option{withGitHub(t, handler)},
		"--config", cfg, "--token", "t", "runner", "remove", "runnerly-01", "--yes")
	if err != nil {
		t.Fatalf("runner remove: %v", err)
	}

	if _, getErr := state.Get(state.Path(cfg), "runnerly-01"); getErr == nil {
		t.Error("the runner is still recorded on this machine")
	}
	// Without --purge the files stay, and the output says where they are.
	if _, statErr := os.Stat(r.Dir); statErr != nil {
		t.Error("remove deleted the install directory without --purge")
	}
	if !strings.Contains(out, r.Dir) {
		t.Errorf("output should name the directory left behind:\n%s", out)
	}
}

func TestRunnerRemovePurgeDeletesTheInstall(t *testing.T) {
	cfg := configIn(t, "")
	r := recordInstalled(t, cfg, "runnerly-01")

	handler := func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"runners":[{"id":7,"name":"runnerly-01"}]}`))
	}

	if _, _, err := runCLI(t, []option{withGitHub(t, handler)},
		"--config", cfg, "--token", "t", "runner", "remove", "runnerly-01", "--purge", "--yes"); err != nil {
		t.Fatalf("runner remove --purge: %v", err)
	}

	if _, statErr := os.Stat(r.Dir); !os.IsNotExist(statErr) {
		t.Error("--purge did not delete the install directory")
	}
}

func TestRunnerRemoveCleansUpAfterGitHubAlreadyForgot(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")

	// GitHub knows nothing about it; the machine still does.
	handler := func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodDelete {
			t.Error("nothing should be deleted from GitHub")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"runners":[]}`))
	}

	out, _, err := runCLI(t, []option{withGitHub(t, handler)},
		"--config", cfg, "--token", "t", "runner", "remove", "runnerly-01", "--yes")
	if err != nil {
		t.Fatalf("runner remove: %v", err)
	}
	if !strings.Contains(out, "already gone") {
		t.Errorf("output should say GitHub had already forgotten it:\n%s", out)
	}
	if _, getErr := state.Get(state.Path(cfg), "runnerly-01"); getErr == nil {
		t.Error("the local record survived")
	}
}
