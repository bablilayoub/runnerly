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
	"time"

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

// Agent configures the machine-side agent.
type Agent struct {
	// EnrollmentToken is the one-shot token used to enroll with the control
	// plane. Prefer RUNNERLY_ENROLLMENT_TOKEN; it is consumed once and the
	// machine token that replaces it is stored separately.
	EnrollmentToken string `yaml:"enrollment_token"`
}

// Ephemeral configures one-job runners.
type Ephemeral struct {
	// LogDir is where a destroyed runner's output is kept. Empty uses a
	// logs directory beside the configuration.
	//
	// It is deliberately outside the runner's own directory: the point of
	// keeping logs is that they survive the runner being destroyed, and
	// GitHub warns that ephemeral runner logs have to be preserved
	// somewhere else to be any use for troubleshooting.
	LogDir string `yaml:"log_dir"`
	// KeepRuns is how many runs of logs to keep. Zero keeps them all, which
	// will eventually fill a disk.
	KeepRuns int `yaml:"keep_runs"`
	// NamePrefix is the start of each generated runner name. Empty uses the
	// machine's hostname.
	NamePrefix string `yaml:"name_prefix"`
}

// Config is the full Runnerly configuration.
type Config struct {
	Server    Server    `yaml:"server"`
	Agent     Agent     `yaml:"agent"`
	Ephemeral Ephemeral `yaml:"ephemeral"`
	GitHub    GitHub    `yaml:"github"`
	Runner    Runner    `yaml:"runner"`
	Executor  Executor  `yaml:"executor"`
	Updates   Updates   `yaml:"updates"`
	Security  Security  `yaml:"security"`
}

// Server points the CLI and agent at a Runnerly control plane, and
// configures the control plane itself. It is optional on a machine that only
// runs the CLI or an agent.
type Server struct {
	// URL is where the control plane can be reached. The agent and CLI use
	// it; the server itself uses it to build OAuth callback URLs.
	URL string `yaml:"url"`
	// Listen is the address runnerly-server binds. It defaults to loopback so
	// a fresh install is not exposed before TLS is in front of it.
	Listen string `yaml:"listen"`
	// Database is the PostgreSQL connection string. Prefer
	// RUNNERLY_DATABASE_URL, which keeps the password out of the file.
	Database string `yaml:"database"`
	// SecretKey encrypts user credentials at rest, hex or base64 encoded,
	// 32 bytes. Prefer RUNNERLY_SECRET_KEY.
	SecretKey string    `yaml:"secret_key"`
	OAuth     OAuth     `yaml:"oauth"`
	Heartbeat Heartbeat `yaml:"heartbeat"`
	TLS       TLS       `yaml:"tls"`
	RateLimit RateLimit `yaml:"rate_limit"`
	Metrics   Metrics   `yaml:"metrics"`
	// EventRetentionDays bounds how long the event feed keeps history. The
	// control plane is not a log platform.
	EventRetentionDays int `yaml:"event_retention_days"`
	// TokenLifetime is how long an agent uses a machine token before the
	// server hands it a replacement. Zero uses the default of 24h.
	//
	// Rotation bounds how long a leaked credential is worth anything. It is
	// not a substitute for revoking one you know has leaked.
	TokenLifetime Duration `yaml:"token_lifetime"`
}

// TLS serves the API over HTTPS directly.
//
// Terminating TLS in a reverse proxy is the more common deployment and
// stays supported; this is for the case where there is nothing in front.
type TLS struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Enabled reports whether the server should serve HTTPS itself.
func (t TLS) Enabled() bool { return t.CertFile != "" && t.KeyFile != "" }

// RateLimit bounds how fast one client can call the API.
//
// The limits are per client address and per minute. Enrollment and sign-in
// get their own, much stricter, because they are the endpoints where
// guessing is worth an attacker's time.
type RateLimit struct {
	// Requests is the general allowance. Zero disables limiting entirely.
	Requests int `yaml:"requests_per_minute"`
	// Auth covers enrollment and sign-in.
	Auth int `yaml:"auth_requests_per_minute"`
	// TrustForwardedFor reads the client address from X-Forwarded-For.
	//
	// Off by default, and it must stay off unless a proxy you control sets
	// that header: anyone can send it, so trusting it lets a client pick
	// its own rate limit bucket.
	TrustForwardedFor bool `yaml:"trust_forwarded_for"`
}

// Metrics configures the Prometheus endpoint.
type Metrics struct {
	// Enabled serves /metrics.
	Enabled bool `yaml:"enabled"`
	// Token, when set, is required as a bearer token to scrape. Empty
	// leaves the endpoint open, which is normal on a private network and is
	// why the server binds to loopback by default.
	Token string `yaml:"token"`
}

// OAuth configures GitHub sign-in for the dashboard.
//
// Runnerly ships no client credentials: a GitHub OAuth App belongs to whoever
// runs the server. Until these are set, the dashboard's sign-in is disabled
// and says what is missing. Prefer RUNNERLY_OAUTH_CLIENT_SECRET for the
// secret.
type OAuth struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// AllowedLogins restricts who may sign in. Empty allows any GitHub
	// account that completes the flow, which is only safe on a server that
	// is not reachable from the internet.
	AllowedLogins []string `yaml:"allowed_logins"`
}

// Heartbeat controls how often agents report and when the server stops
// believing them.
type Heartbeat struct {
	// Interval is how often an agent sends a heartbeat.
	Interval Duration `yaml:"interval"`
	// StaleAfter is how long without one before a runner is reported stale.
	StaleAfter Duration `yaml:"stale_after"`
	// OfflineAfter is how long before it is reported offline.
	OfflineAfter Duration `yaml:"offline_after"`
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
	Type   ExecutorType `yaml:"type"`
	Docker Docker       `yaml:"docker"`
}

// Cleanup policies for what a job leaves behind in Docker.
const (
	// CleanupNever leaves everything in place.
	CleanupNever = "never"
	// CleanupAfterJob removes what appeared during the job.
	CleanupAfterJob = "after_job"
)

// Docker configures the Docker executor.
//
// It does not make jobs run in containers: whether a job is containerized is
// decided by the workflow's own `container:` and `services:` keys. What this
// controls is the Docker environment those keys rely on, and what happens to
// what they leave behind.
type Docker struct {
	// Host overrides DOCKER_HOST for the runner, for a non-default socket or
	// a rootless daemon. Empty uses Docker's own default.
	Host string `yaml:"host"`
	// Cleanup decides what to remove when a job finishes: "after_job" or
	// "never".
	//
	// Cleanup only removes what appeared while the job ran. A shared machine
	// running other containers keeps them.
	Cleanup string `yaml:"cleanup"`
	// PruneImages also removes dangling images after a job. Off by default:
	// it reclaims disk at the cost of re-pulling layers on the next job.
	PruneImages bool `yaml:"prune_images"`
	// Timeout bounds each docker command the agent runs. Zero uses a
	// sensible default.
	Timeout Duration `yaml:"timeout"`
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
		Server: Server{
			Listen:             "127.0.0.1:8080",
			EventRetentionDays: 30,
			// An empty slice rather than nil, so the documented
			// `allowed_logins: []` in the template round-trips to exactly
			// these defaults.
			OAuth:         OAuth{AllowedLogins: []string{}},
			Metrics:       Metrics{Enabled: true},
			TokenLifetime: Duration(24 * time.Hour),
			RateLimit:     RateLimit{Requests: 600, Auth: 20},
			Heartbeat: Heartbeat{
				Interval:     Duration(20 * time.Second),
				StaleAfter:   Duration(30 * time.Second),
				OfflineAfter: Duration(90 * time.Second),
			},
		},
		GitHub: GitHub{
			Host:  "github.com",
			Scope: ScopeRepository,
		},
		Runner: Runner{
			Labels: defaultLabels(),
		},
		Executor: Executor{
			Type: ExecutorDocker,
			Docker: Docker{
				Cleanup: CleanupAfterJob,
				Timeout: Duration(2 * time.Minute),
			},
		},
		Ephemeral: Ephemeral{KeepRuns: 50},
		Updates:   Updates{Auto: false},
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

// CleanupAfterJob reports whether the Docker executor should tidy up when a
// job finishes.
func (c Config) CleanupAfterJob() bool {
	if c.Executor.Type != ExecutorDocker {
		return false
	}
	// Empty means the default, which is to clean up.
	return c.Executor.Docker.Cleanup != CleanupNever
}

// EphemeralLogDir returns where a destroyed runner's logs are kept.
func (c Config) EphemeralLogDir(configPath string) string {
	if c.Ephemeral.LogDir != "" {
		return c.Ephemeral.LogDir
	}
	return filepath.Join(filepath.Dir(configPath), "logs")
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

	switch cfg.Executor.Docker.Cleanup {
	case "", CleanupNever, CleanupAfterJob:
	default:
		return fmt.Errorf("executor.docker.cleanup: %q is not valid (use %q or %q)",
			cfg.Executor.Docker.Cleanup, CleanupAfterJob, CleanupNever)
	}
	if cfg.Executor.Docker.Timeout < 0 {
		return errors.New("executor.docker.timeout: must not be negative")
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

	hb := cfg.Server.Heartbeat
	if hb.Interval < 0 || hb.StaleAfter < 0 || hb.OfflineAfter < 0 {
		return errors.New("server.heartbeat: durations must not be negative")
	}
	if hb.StaleAfter > 0 && hb.OfflineAfter > 0 && hb.OfflineAfter <= hb.StaleAfter {
		return fmt.Errorf("server.heartbeat: offline_after (%s) must be longer than stale_after (%s)",
			hb.OfflineAfter, hb.StaleAfter)
	}
	if hb.Interval > 0 && hb.StaleAfter > 0 && hb.Interval >= hb.StaleAfter {
		return fmt.Errorf("server.heartbeat: interval (%s) must be shorter than stale_after (%s), "+
			"or every runner will look stale between heartbeats", hb.Interval, hb.StaleAfter)
	}
	if cfg.Server.EventRetentionDays < 0 {
		return errors.New("server.event_retention_days: must not be negative")
	}

	if cfg.Server.TokenLifetime < 0 {
		return errors.New("server.token_lifetime: must not be negative (0 uses the default)")
	}
	if cfg.Server.RateLimit.Requests < 0 || cfg.Server.RateLimit.Auth < 0 {
		return errors.New("server.rate_limit: rates must not be negative (0 disables limiting)")
	}
	if (cfg.Server.TLS.CertFile == "") != (cfg.Server.TLS.KeyFile == "") {
		return errors.New("server.tls: set both cert_file and key_file, or neither")
	}

	if cfg.Ephemeral.KeepRuns < 0 {
		return errors.New("ephemeral.keep_runs: must not be negative (0 keeps every run)")
	}
	if cfg.Ephemeral.NamePrefix != "" && !runnerNamePattern.MatchString(cfg.Ephemeral.NamePrefix) {
		return fmt.Errorf("ephemeral.name_prefix: %q may only contain letters, digits, '.', '_' and '-'",
			cfg.Ephemeral.NamePrefix)
	}

	return nil
}

// Duration is a time.Duration that reads from YAML as "30s" or "2m".
type Duration time.Duration

// UnmarshalYAML accepts a duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("a duration must be a string like \"30s\": %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("%q is not a duration (use a form like 20s, 2m or 1h)", raw)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML writes the duration back as a string.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

// Duration returns the value as a time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }
