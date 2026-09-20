// Package config loads and validates the Runnerly configuration file.
//
// Resolution order for the config path:
//
//  1. an explicit path passed on the command line
//  2. $RUNNERLY_CONFIG
//  3. $XDG_CONFIG_HOME/runnerly/config.yaml (or ~/.config/runnerly/config.yaml)
//  4. /etc/runnerly/config.yaml, if it exists
//
// A missing file is not an error: Load returns defaults so that commands such
// as `runnerly doctor` work on a machine that has never been configured.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// SystemPath is the machine-wide configuration file, used when no per-user
// file exists.
const SystemPath = "/etc/runnerly/config.yaml"

// ExecutorType selects how workflow jobs are executed on a runner machine.
type ExecutorType string

const (
	// ExecutorHost runs the official GitHub runner directly on the host.
	ExecutorHost ExecutorType = "host"
	// ExecutorDocker runs the official GitHub runner with Docker available
	// for containerized steps and job containers.
	ExecutorDocker ExecutorType = "docker"
)

// Config is the full Runnerly configuration.
type Config struct {
	Server   Server   `yaml:"server"`
	GitHub   GitHub   `yaml:"github"`
	Runner   Runner   `yaml:"runner"`
	Executor Executor `yaml:"executor"`
	Updates  Updates  `yaml:"updates"`
	Security Security `yaml:"security"`
}

// Server points the CLI and agent at a Runnerly control plane. It is
// optional: the CLI is usable without one.
type Server struct {
	URL string `yaml:"url"`
}

// GitHub identifies the GitHub deployment to talk to.
type GitHub struct {
	// Host is the GitHub hostname, e.g. "github.com". GitHub Enterprise
	// Server hosts are accepted but not yet exercised.
	Host string `yaml:"host"`
	// Scope is "repository" or "organization".
	Scope string `yaml:"scope"`
	// Repository is "owner/repo" when Scope is "repository".
	Repository string `yaml:"repository"`
	// Organization is the org login when Scope is "organization".
	Organization string `yaml:"organization"`
}

// Runner describes the self-hosted runner this machine registers.
type Runner struct {
	Name   string   `yaml:"name"`
	Labels []string `yaml:"labels"`
	// Dir is where the official GitHub runner is installed. Empty means the
	// default for the current user, which DefaultRunnerDir computes.
	Dir string `yaml:"dir"`
}

// Executor selects the execution mode.
type Executor struct {
	Type ExecutorType `yaml:"type"`
}

// Updates controls how Runnerly upgrades itself and the GitHub runner.
type Updates struct {
	Auto bool `yaml:"auto"`
}

// Security holds the policies described in docs/security.md. They are
// currently advisory: Runnerly surfaces them, it does not yet enforce them
// against GitHub.
type Security struct {
	AllowPublicRepositories bool `yaml:"allow_public_repositories"`
	AllowForkWorkflows      bool `yaml:"allow_fork_workflows"`
}

// Scope values for GitHub.Scope.
const (
	ScopeRepository   = "repository"
	ScopeOrganization = "organization"
)

// Default returns the configuration used when no file exists.
func Default() Config {
	return Config{
		GitHub: GitHub{
			Host:  "github.com",
			Scope: ScopeRepository,
		},
		Runner: Runner{
			Labels: defaultLabels(),
		},
		Executor: Executor{Type: ExecutorDocker},
		Updates:  Updates{Auto: false},
		Security: Security{
			AllowPublicRepositories: false,
			AllowForkWorkflows:      false,
		},
	}
}

// defaultLabels mirrors the labels GitHub's own runner applies, plus a marker
// so Runnerly-managed runners are identifiable in the GitHub UI.
func defaultLabels() []string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	return []string{runtime.GOOS, arch, "runnerly"}
}

// DefaultRunnerDir returns where the official GitHub runner is installed when
// runner.dir is empty.
//
// Root installs system-wide so a systemd unit can find it; an unprivileged
// user installs under their own data directory, because Runnerly never writes
// outside what the invoking user already owns.
func DefaultRunnerDir() string {
	if os.Geteuid() == 0 {
		return "/opt/runnerly/runners"
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "runnerly", "runners")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "runnerly", "runners")
	}
	return filepath.Join(home, ".local", "share", "runnerly", "runners")
}

// RunnerDir returns the directory a named runner is installed in.
func (c Config) RunnerDir(name string) string {
	base := c.Runner.Dir
	if base == "" {
		base = DefaultRunnerDir()
	}
	return filepath.Join(base, name)
}

// Path returns the configuration file path that Load would read when given an
// empty explicit path.
func Path() string {
	if p := os.Getenv("RUNNERLY_CONFIG"); p != "" {
		return p
	}
	user := userPath()
	if user == "" {
		return SystemPath
	}
	if _, err := os.Stat(user); err != nil {
		if _, sysErr := os.Stat(SystemPath); sysErr == nil {
			return SystemPath
		}
	}
	return user
}

func userPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "runnerly", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "runnerly", "config.yaml")
}

// Load reads the configuration file. An empty path uses Path(). When the file
// does not exist, defaults are returned with found=false and no error.
func Load(path string) (cfg Config, found bool, err error) {
	if path == "" {
		path = Path()
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied by design
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), false, nil
		}
		return Default(), false, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg = Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// An empty file decodes to io.EOF; treat it as "no overrides".
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Default(), false, fmt.Errorf("parse config %s: %w", path, err)
	}
	if len(cfg.Runner.Labels) == 0 {
		cfg.Runner.Labels = defaultLabels()
	}
	return cfg, true, nil
}

// Save writes the configuration to path, creating parent directories. The file
// is written with 0600 because it may later hold credentials.
//
// Save marshals the struct, so comments are not preserved. `config init` uses
// Template instead, to produce a file an operator can read.
func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return write(path, data)
}

// WriteTemplate writes the commented default configuration to path.
func WriteTemplate(path string) error {
	return write(path, []byte(Template()))
}

func write(path string, data []byte) error {
	if path == "" {
		path = Path()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}

// labelPattern matches the characters GitHub accepts in a runner label.
var labelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// runnerNamePattern is deliberately conservative; the name also becomes a
// directory name on the runner machine.
var runnerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Validate reports the first problem that would stop Runnerly from using
// this configuration.
func Validate(cfg Config) error {
	switch cfg.Executor.Type {
	case ExecutorHost, ExecutorDocker:
	default:
		return fmt.Errorf("executor.type: %q is not valid (use %q or %q)",
			cfg.Executor.Type, ExecutorHost, ExecutorDocker)
	}

	if cfg.GitHub.Host == "" {
		return errors.New("github.host: must not be empty (use github.com)")
	}

	switch cfg.GitHub.Scope {
	case "", ScopeRepository:
		if cfg.GitHub.Repository != "" && !strings.Contains(strings.Trim(cfg.GitHub.Repository, "/"), "/") {
			return fmt.Errorf("github.repository: %q must be in owner/repo form", cfg.GitHub.Repository)
		}
	case ScopeOrganization:
		if cfg.GitHub.Organization == "" {
			return errors.New("github.organization: required when github.scope is organization")
		}
	default:
		return fmt.Errorf("github.scope: %q is not valid (use %q or %q)",
			cfg.GitHub.Scope, ScopeRepository, ScopeOrganization)
	}

	if cfg.Runner.Name != "" && !runnerNamePattern.MatchString(cfg.Runner.Name) {
		return fmt.Errorf("runner.name: %q may only contain letters, digits, '.', '_' and '-'", cfg.Runner.Name)
	}

	for _, l := range cfg.Runner.Labels {
		if !labelPattern.MatchString(l) {
			return fmt.Errorf("runner.labels: %q is not a valid GitHub runner label", l)
		}
	}

	if cfg.Server.URL != "" {
		u, err := url.Parse(cfg.Server.URL)
		if err != nil {
			return fmt.Errorf("server.url: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("server.url: %q must start with http:// or https://", cfg.Server.URL)
		}
		if u.Host == "" {
			return fmt.Errorf("server.url: %q is missing a host", cfg.Server.URL)
		}
	}

	return nil
}
