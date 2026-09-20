package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Validate(Default()); err != nil {
		t.Fatalf("Default() is not valid: %v", err)
	}
}

func TestDefaultLabelsNormalizeAmd64ToX64(t *testing.T) {
	labels := defaultLabels()
	if labels[0] != runtime.GOOS {
		t.Errorf("first label = %q, want %q", labels[0], runtime.GOOS)
	}
	want := runtime.GOARCH
	if want == "amd64" {
		want = "x64"
	}
	if labels[1] != want {
		t.Errorf("arch label = %q, want %q", labels[1], want)
	}
	if labels[len(labels)-1] != "runnerly" {
		t.Errorf("missing runnerly marker label: %v", labels)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, found, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if found {
		t.Error("found = true for a missing file")
	}
	if cfg.Executor.Type != ExecutorDocker {
		t.Errorf("Executor.Type = %q, want %q", cfg.Executor.Type, ExecutorDocker)
	}
}

func TestLoadAppliesOverridesOnTopOfDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, `
executor:
  type: host
runner:
  name: runnerly-01
github:
  repository: bablilayoub/example
`)
	cfg, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found {
		t.Error("found = false for an existing file")
	}
	if cfg.Executor.Type != ExecutorHost {
		t.Errorf("Executor.Type = %q, want host", cfg.Executor.Type)
	}
	if cfg.Runner.Name != "runnerly-01" {
		t.Errorf("Runner.Name = %q", cfg.Runner.Name)
	}
	// Untouched keys keep their defaults.
	if cfg.GitHub.Host != "github.com" {
		t.Errorf("GitHub.Host = %q, want github.com", cfg.GitHub.Host)
	}
	if len(cfg.Runner.Labels) == 0 {
		t.Error("Runner.Labels lost its default")
	}
}

func TestLoadEmptyFileIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, "")
	cfg, found, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !found {
		t.Error("found = false for an existing empty file")
	}
	if cfg.GitHub.Host != "github.com" {
		t.Errorf("empty file did not fall back to defaults: %+v", cfg)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, "executer:\n  type: host\n")
	if _, _, err := Load(path); err == nil {
		t.Fatal("Load() accepted a misspelled key")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	want := Default()
	want.Runner.Name = "runnerly-07"
	want.Runner.Labels = []string{"linux", "x64", "docker"}
	want.Server.URL = "https://runnerly.internal"
	want.GitHub.Repository = "bablilayoub/example"

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config permissions = %o, want 600", perm)
	}

	got, found, err := Load(path)
	if err != nil || !found {
		t.Fatalf("Load() = %v, found=%v", err, found)
	}
	if got.Runner.Name != want.Runner.Name || got.Server.URL != want.Server.URL {
		t.Errorf("round trip mismatch:\ngot  %+v\nwant %+v", got, want)
	}
	if strings.Join(got.Runner.Labels, ",") != "linux,x64,docker" {
		t.Errorf("labels = %v", got.Runner.Labels)
	}
}

func TestPathPrefersEnvironmentOverride(t *testing.T) {
	t.Setenv("RUNNERLY_CONFIG", "/tmp/custom.yaml")
	if got := Path(); got != "/tmp/custom.yaml" {
		t.Errorf("Path() = %q, want the RUNNERLY_CONFIG value", got)
	}
}

func TestPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("RUNNERLY_CONFIG", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	want := filepath.Join(dir, "runnerly", "config.yaml")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"unknown executor", func(c *Config) { c.Executor.Type = "podman" }, "executor.type"},
		{"empty github host", func(c *Config) { c.GitHub.Host = "" }, "github.host"},
		{"unknown scope", func(c *Config) { c.GitHub.Scope = "enterprise" }, "github.scope"},
		{"org scope without org", func(c *Config) { c.GitHub.Scope = ScopeOrganization }, "github.organization"},
		{"repository without slash", func(c *Config) { c.GitHub.Repository = "example" }, "owner/repo"},
		{"bad runner name", func(c *Config) { c.Runner.Name = "my runner" }, "runner.name"},
		{"bad label", func(c *Config) { c.Runner.Labels = []string{"has space"} }, "runner.labels"},
		{"server url without scheme", func(c *Config) { c.Server.URL = "runnerly.internal" }, "http://"},
		{"server url without host", func(c *Config) { c.Server.URL = "https://" }, "missing a host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			err := Validate(cfg)
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAcceptsOrganizationScope(t *testing.T) {
	cfg := Default()
	cfg.GitHub.Scope = ScopeOrganization
	cfg.GitHub.Organization = "bablilayoub"
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
