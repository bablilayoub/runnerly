package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/jobstate"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/supervisor"
)

// installedRunner creates a directory that looks like a configured runner.
func installedRunner(t *testing.T, name string) state.Runner {
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
	return state.Runner{
		Name:   name,
		Dir:    dir,
		Host:   "github.com",
		Scope:  github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Labels: []string{"self-hosted", "linux", "x64"},
	}
}

// logLines parses JSON log output into maps.
func logLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, line)
		}
		entries = append(entries, m)
	}
	return entries
}

func eventNames(entries []map[string]any) []string {
	var out []string
	for _, e := range entries {
		if v, ok := e["event"].(string); ok {
			out = append(out, v)
		}
	}
	return out
}

func TestPreflightAcceptsAnInstalledRunner(t *testing.T) {
	r := installedRunner(t, "runnerly-01")
	script, err := Preflight(r)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !strings.HasSuffix(script, "run.sh") || !strings.HasPrefix(script, ".") {
		t.Errorf("script = %q, want a path relative to the runner directory", script)
	}
}

func TestPreflightRejectsWhatItCannotRun(t *testing.T) {
	missingDir := state.Runner{Name: "gone", Dir: filepath.Join(t.TempDir(), "absent")}

	unconfigured := installedRunner(t, "unconfigured")
	if err := os.Remove(filepath.Join(unconfigured.Dir, ".runner")); err != nil {
		t.Fatal(err)
	}

	noScript := installedRunner(t, "no-script")
	if err := os.Remove(filepath.Join(noScript.Dir, "run.sh")); err != nil {
		t.Fatal(err)
	}

	notADir := state.Runner{Name: "file", Dir: filepath.Join(t.TempDir(), "afile")}
	if err := os.WriteFile(notADir.Dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		runner state.Runner
		want   string
	}{
		{"no name", state.Runner{Dir: "/tmp"}, "needs a runner name"},
		{"no directory", state.Runner{Name: "x"}, "no installation directory"},
		{"directory is gone", missingDir, "that directory is gone"},
		{"not a directory", notADir, "not a directory"},
		{"never registered", unconfigured, "no configured runner"},
		{"no run script", noScript, "no run.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Preflight(tt.runner)
			if err == nil {
				t.Fatal("Preflight() accepted a runner it cannot start")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestPreflightErrorsSayHowToRecover(t *testing.T) {
	r := installedRunner(t, "runnerly-01")
	if err := os.Remove(filepath.Join(r.Dir, ".runner")); err != nil {
		t.Fatal(err)
	}
	_, err := Preflight(r)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "runnerly runner create") {
		t.Errorf("error should offer a next step, got: %v", err)
	}
}

// fakeProcess is a process the test controls.
type fakeProcess struct {
	mu     sync.Mutex
	exited bool
	exitCh chan error
}

func newFakeProcess() *fakeProcess { return &fakeProcess{exitCh: make(chan error, 1)} }

func (f *fakeProcess) finish(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.exited {
		f.exited = true
		f.exitCh <- err
	}
}

func (f *fakeProcess) Wait() error { return <-f.exitCh }
func (f *fakeProcess) Stop() error { f.finish(nil); return nil }
func (f *fakeProcess) Kill() error { f.finish(errors.New("killed")); return nil }
func (f *fakeProcess) PID() int    { return 4242 }

func TestRunLogsTheRunnerLifecycle(t *testing.T) {
	r := installedRunner(t, "runnerly-01")
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	proc := newFakeProcess()
	started := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgent(ctx, Options{
			Runner: r,
			Logger: logger,
			Start: func(opts supervisor.ProcessOptions) (supervisor.Process, error) {
				if opts.Dir != r.Dir {
					t.Errorf("working directory = %q, want %q", opts.Dir, r.Dir)
				}
				close(started)
				return proc, nil
			},
		})
	}()

	<-started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil after cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return")
	}

	entries := logLines(t, buf.String())
	events := eventNames(entries)
	for _, want := range []string{"agent_started", "runner_online", "runner_stopping", "runner_offline", "agent_stopped"} {
		if !slicesContains(events, want) {
			t.Errorf("missing event %q in %v", want, events)
		}
	}

	// Plan section 28: every line carries the component and the runner.
	for _, e := range entries {
		if e["component"] != Component {
			t.Errorf("log line missing component: %v", e)
		}
		if e["runner"] != "runnerly-01" {
			t.Errorf("log line missing runner: %v", e)
		}
		if _, ok := e["time"]; !ok {
			t.Errorf("log line missing time: %v", e)
		}
		if _, ok := e["level"]; !ok {
			t.Errorf("log line missing level: %v", e)
		}
	}
}

func TestRunReportsRestartsAndGivingUp(t *testing.T) {
	r := installedRunner(t, "runnerly-01")
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	err := runAgent(context.Background(), Options{
		Runner:  r,
		Logger:  logger,
		Backoff: supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 2},
		Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
			p := newFakeProcess()
			p.finish(errors.New("crashed"))
			return p, nil
		},
	})
	if !errors.Is(err, supervisor.ErrGaveUp) {
		t.Fatalf("Run() = %v, want ErrGaveUp", err)
	}

	entries := logLines(t, buf.String())
	events := eventNames(entries)
	for _, want := range []string{"runner_exited", "runner_restarting", "runner_failed", "agent_failed"} {
		if !slicesContains(events, want) {
			t.Errorf("missing event %q in %v", want, events)
		}
	}

	// A restart is a warning and giving up is an error, so an operator
	// filtering on severity sees the right things.
	for _, e := range entries {
		switch e["event"] {
		case "runner_restarting":
			if e["level"] != "WARN" {
				t.Errorf("runner_restarting logged at %v, want WARN", e["level"])
			}
		case "runner_failed", "agent_failed":
			if e["level"] != "ERROR" {
				t.Errorf("%v logged at %v, want ERROR", e["event"], e["level"])
			}
		}
	}
}

func TestEphemeralRunnerStopsAfterACleanExit(t *testing.T) {
	r := installedRunner(t, "ephemeral-01")
	r.Ephemeral = true

	starts := 0
	err := runAgent(context.Background(), Options{
		Runner:  r,
		Backoff: supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 3},
		Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
			starts++
			p := newFakeProcess()
			p.finish(nil)
			return p, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if starts != 1 {
		t.Errorf("started %d times, want 1 for an ephemeral runner", starts)
	}
}

func TestRunRefusesAnUninstalledRunner(t *testing.T) {
	err := runAgent(context.Background(), Options{
		Runner: state.Runner{Name: "ghost", Dir: filepath.Join(t.TempDir(), "absent")},
	})
	if err == nil {
		t.Fatal("Run() started an agent for a runner that is not installed")
	}
}

func TestRunnerOutputIsKeptOutOfTheAgentLog(t *testing.T) {
	r := installedRunner(t, "runnerly-01")
	var agentLog, runnerOut bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&agentLog, nil))

	err := runAgent(context.Background(), Options{
		Runner:  r,
		Logger:  logger,
		Output:  &runnerOut,
		Backoff: supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 1},
		Start: func(opts supervisor.ProcessOptions) (supervisor.Process, error) {
			if opts.Stdout != &runnerOut {
				t.Error("the runner's stdout was not wired to Output")
			}
			p := newFakeProcess()
			p.finish(errors.New("crashed"))
			return p, nil
		},
	})
	if !errors.Is(err, supervisor.ErrGaveUp) {
		t.Fatalf("Run() = %v", err)
	}
	// Every agent line must still be parseable JSON, which it would not be if
	// unstructured runner output were interleaved.
	logLines(t, agentLog.String())
}

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer

	jsonLogger, err := NewLogger(&buf, FormatJSON, "info")
	if err != nil {
		t.Fatalf("NewLogger(json) error = %v", err)
	}
	jsonLogger.Info("hello", "event", "test")
	if !strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Errorf("json output = %q", buf.String())
	}

	buf.Reset()
	textLogger, err := NewLogger(&buf, FormatText, "info")
	if err != nil {
		t.Fatalf("NewLogger(text) error = %v", err)
	}
	textLogger.Info("hello", "event", "test")
	if !strings.Contains(buf.String(), "event=test") {
		t.Errorf("text output = %q", buf.String())
	}

	buf.Reset()
	debugLogger, err := NewLogger(&buf, "", "debug")
	if err != nil {
		t.Fatalf("NewLogger(default) error = %v", err)
	}
	debugLogger.Debug("visible")
	if buf.Len() == 0 {
		t.Error("debug level did not emit a debug line")
	}

	if _, err := NewLogger(&buf, "xml", "info"); err == nil {
		t.Error("NewLogger() accepted an unknown format")
	}
	if _, err := NewLogger(&buf, FormatJSON, "chatty"); err == nil {
		t.Error("NewLogger() accepted an unknown level")
	}
}

func TestParseLevel(t *testing.T) {
	levels := map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for in, want := range levels {
		got, err := ParseLevel(in)
		if err != nil {
			t.Errorf("ParseLevel(%q) error = %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// TestACleanExitIsStillAWarningForALongLivedRunner records what a live run
// showed: killing the runner's listener makes run.sh exit 0, so a crash and a
// clean stop are indistinguishable by exit code. A runner that is meant to
// stay up gets a warning either way.
func TestACleanExitIsStillAWarningForALongLivedRunner(t *testing.T) {
	r := installedRunner(t, "runnerly-01")
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	err := runAgent(context.Background(), Options{
		Runner:  r,
		Logger:  logger,
		Backoff: supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 1},
		Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
			p := newFakeProcess()
			p.finish(nil) // exit code 0
			return p, nil
		},
	})
	if !errors.Is(err, supervisor.ErrGaveUp) {
		t.Fatalf("Run() = %v, want ErrGaveUp", err)
	}

	for _, e := range logLines(t, buf.String()) {
		if e["event"] == "runner_exited" && e["level"] != "WARN" {
			t.Errorf("a clean exit logged at %v, want WARN for a long-lived runner", e["level"])
		}
	}
}

func TestAnEphemeralRunnerFinishingIsNotAWarning(t *testing.T) {
	r := installedRunner(t, "ephemeral-01")
	r.Ephemeral = true

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	if err := runAgent(context.Background(), Options{
		Runner: r,
		Logger: logger,
		Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
			p := newFakeProcess()
			p.finish(nil)
			return p, nil
		},
	}); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	var sawExit bool
	for _, e := range logLines(t, buf.String()) {
		if e["event"] == "runner_exited" {
			sawExit = true
			if e["level"] != "INFO" {
				t.Errorf("an ephemeral runner finishing logged at %v, want INFO", e["level"])
			}
		}
	}
	if !sawExit {
		t.Error("no exit was logged")
	}
}

// errProcessCrashed stands in for a runner that died.
var errProcessCrashed = errors.New("crashed")

// testLogger discards output but is not nil, so code under test logs freely.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestConfigureOnlySetsUpHooksForTheDockerExecutor(t *testing.T) {
	host := config.Default()
	host.Executor.Type = config.ExecutorHost

	hooks, env := Configure(host, "/etc/runnerly/config.yaml")
	if hooks.Install {
		t.Error("the host executor installed job hooks; there is nothing to clean up")
	}
	if len(env) != 0 {
		t.Errorf("env = %v, want nothing for the host executor", env)
	}
}

func TestConfigureForDocker(t *testing.T) {
	cfg := config.Default()
	cfg.Executor.Type = config.ExecutorDocker
	cfg.Executor.Docker.Host = "unix:///run/user/1000/docker.sock"

	hooks, env := Configure(cfg, "/etc/runnerly/config.yaml")
	if !hooks.Install {
		t.Fatal("the Docker executor did not install job hooks")
	}
	if hooks.ConfigPath != "/etc/runnerly/config.yaml" {
		t.Errorf("ConfigPath = %q", hooks.ConfigPath)
	}
	if hooks.Binary == "" {
		t.Error("the hooks have no binary to call")
	}
	if !filepath.IsAbs(hooks.Binary) {
		t.Errorf("Binary = %q, want an absolute path: hooks run with no known working directory", hooks.Binary)
	}

	// The runner starts job containers itself, so it needs the same daemon.
	if len(env) != 1 || !strings.Contains(env[0], "DOCKER_HOST=unix:///run/user/1000/docker.sock") {
		t.Errorf("env = %v", env)
	}
}

func TestRunnerInheritsTheEnvironmentPlusWhatRunnerlyAdds(t *testing.T) {
	r := installedRunner(t, "runnerly-01")
	t.Setenv("RUNNERLY_TEST_MARKER", "present")

	var got []string
	err := runAgent(context.Background(), Options{
		Runner:   r,
		ExtraEnv: []string{"DOCKER_HOST=unix:///custom.sock"},
		Backoff:  supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 1},
		Start: func(opts supervisor.ProcessOptions) (supervisor.Process, error) {
			got = opts.Env
			p := newFakeProcess()
			p.finish(errProcessCrashed)
			return p, nil
		},
	})
	if !errors.Is(err, supervisor.ErrGaveUp) {
		t.Fatalf("Run() = %v", err)
	}

	joined := strings.Join(got, "\n")
	// A build needs PATH and everything else, so the runner's environment
	// starts from the agent's rather than replacing it.
	if !strings.Contains(joined, "RUNNERLY_TEST_MARKER=present") {
		t.Error("the runner did not inherit the agent's environment")
	}
	if !strings.Contains(joined, "DOCKER_HOST=unix:///custom.sock") {
		t.Error("the extra environment was not passed to the runner")
	}
}

func TestHooksAreInstalledAndPointedAt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hooks are not implemented for Windows runners")
	}
	r := installedRunner(t, "runnerly-01")

	var got []string
	err := runAgent(context.Background(), Options{
		Runner:  r,
		Hooks:   HookOptions{Install: true, Binary: "/usr/local/bin/runnerly", ConfigPath: "/etc/c.yaml"},
		Backoff: supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 1},
		Start: func(opts supervisor.ProcessOptions) (supervisor.Process, error) {
			got = opts.Env
			p := newFakeProcess()
			p.finish(errProcessCrashed)
			return p, nil
		},
	})
	if !errors.Is(err, supervisor.ErrGaveUp) {
		t.Fatalf("Run() = %v", err)
	}

	joined := strings.Join(got, "\n")
	for _, want := range []string{jobstate.HookStartedEnv, jobstate.HookCompletedEnv} {
		if !strings.Contains(joined, want) {
			t.Errorf("the runner was not pointed at %s", want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(jobstate.StateDir(r.Dir), "job-started.sh")); statErr != nil {
		t.Errorf("the started hook was not written: %v", statErr)
	}
}

func TestAFailedHookInstallDoesNotStopTheRunner(t *testing.T) {
	r := installedRunner(t, "runnerly-01")

	// No binary path, so installing the hooks fails. The runner must still
	// run: unobserved is better than not running at all.
	started := false
	err := runAgent(context.Background(), Options{
		Runner:  r,
		Hooks:   HookOptions{Install: true, ConfigPath: "/etc/c.yaml"},
		Backoff: supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 1},
		Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
			started = true
			p := newFakeProcess()
			p.finish(errProcessCrashed)
			return p, nil
		},
	})
	if !errors.Is(err, supervisor.ErrGaveUp) {
		t.Fatalf("Run() = %v", err)
	}
	if !started {
		t.Error("the runner never started because the hooks could not be installed")
	}
}

func TestBusyIsReportedFromTheJobHooks(t *testing.T) {
	fake := newFakeControlPlane(t)
	r := installedRunner(t, "runnerly-01")
	statePath := jobstate.Path(r.Dir)

	if err := jobstate.Write(statePath, jobstate.State{
		Status:     jobstate.StatusRunning,
		Repository: "acme/widgets",
		Workflow:   "test",
	}); err != nil {
		t.Fatal(err)
	}

	proc := newFakeProcess()
	started := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgent(ctx, Options{
			Runner:            r,
			ControlPlane:      fake.client(),
			HeartbeatInterval: 10 * time.Millisecond,
			Hooks:             HookOptions{Install: true, Binary: "/usr/local/bin/runnerly"},
			Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
				close(started)
				return proc, nil
			},
		})
	}()

	<-started
	// Give a few heartbeats time to go out.
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	var sawBusy bool
	for _, status := range fake.statuses() {
		if status == statusBusy {
			sawBusy = true
		}
	}
	if !sawBusy {
		t.Errorf("the runner never reported busy while a job was running: %v", fake.statuses())
	}
}

func TestWithoutHooksTheAgentDoesNotGuessBusy(t *testing.T) {
	fake := newFakeControlPlane(t)
	r := installedRunner(t, "runnerly-01")

	// A job file exists, but hooks are off, so the agent has no business
	// reading it and must not claim to know.
	if err := jobstate.Write(jobstate.Path(r.Dir), jobstate.State{Status: jobstate.StatusRunning}); err != nil {
		t.Fatal(err)
	}

	proc := newFakeProcess()
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgent(ctx, Options{
			Runner:            r,
			ControlPlane:      fake.client(),
			HeartbeatInterval: 10 * time.Millisecond,
			Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
				close(started)
				return proc, nil
			},
		})
	}()

	<-started
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	for _, status := range fake.statuses() {
		if status == statusBusy {
			t.Error("the agent reported busy without job hooks installed")
		}
	}
}

// runAgent is Run with the outcome dropped, for the tests that only care
// whether it failed.
func runAgent(ctx context.Context, opts Options) error {
	_, err := Run(ctx, opts)
	return err
}
