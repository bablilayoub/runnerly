// Package doctor inspects a machine and reports whether it can host an
// Runnerly-managed GitHub Actions runner.
//
// Every check is pure with respect to an Env, so the whole suite runs against
// fakes in tests. Doctor never needs a Runnerly control plane: checks that
// depend on one are skipped rather than failed.
package doctor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/github"
)

// Status is the outcome of a single check.
type Status string

const (
	// StatusPass means the check succeeded.
	StatusPass Status = "pass"
	// StatusWarn means the machine works but something is worth knowing.
	StatusWarn Status = "warn"
	// StatusFail means Runnerly cannot work until this is fixed.
	StatusFail Status = "fail"
	// StatusSkip means the check did not apply to this machine or run.
	StatusSkip Status = "skip"
)

// Check is one diagnostic result.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	// Detail explains what was found, in one or more lines.
	Detail string `json:"detail,omitempty"`
	// Remedy is a command or action the operator can take verbatim.
	Remedy string `json:"remedy,omitempty"`
}

// Report is the result of a full doctor run.
type Report struct {
	Checks  []Check `json:"checks"`
	Summary Summary `json:"summary"`
}

// Summary counts checks by status.
type Summary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

// OK reports whether the machine is usable, i.e. nothing failed.
func (r Report) OK() bool { return r.Summary.Fail == 0 }

// Env is the outside world, injected so the suite is testable.
type Env struct {
	GOOS       string
	GOARCH     string
	LookPath   func(file string) (string, error)
	Run        func(ctx context.Context, name string, args ...string) ([]byte, error)
	HTTPClient *http.Client
	Dial       func(ctx context.Context, network, addr string) (net.Conn, error)
}

// DefaultEnv returns an Env wired to the real machine.
func DefaultEnv() Env {
	return Env{
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			// #nosec G204 -- this is the single, deliberate place where doctor
			// shells out. Command names are constants in this package; the
			// indirection exists so tests can substitute a fake.
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		HTTPClient: &http.Client{},
		Dial:       (&net.Dialer{}).DialContext,
	}
}

// Options configures a doctor run.
type Options struct {
	Config config.Config
	// ConfigPath is reported to the operator so they know which file is live.
	ConfigPath string
	// ConfigFound is false when no configuration file exists yet.
	ConfigFound bool
	// Token is the resolved GitHub token, empty when none was found. Doctor
	// never reads credentials itself; the CLI resolves them and passes the
	// result in, so a doctor run is reproducible from its Options alone.
	Token string
	// TokenOrigin names where Token came from, for the report.
	TokenOrigin string
	// Offline skips every check that needs the network.
	Offline bool
	// Timeout bounds each individual check. Zero means DefaultTimeout.
	Timeout time.Duration
	Env     Env
}

// DefaultTimeout bounds each individual check.
const DefaultTimeout = 5 * time.Second

// Run executes every check and returns a report.
func Run(ctx context.Context, opts Options) Report {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Env.LookPath == nil {
		opts.Env = DefaultEnv()
	}

	checks := []Check{
		checkConfiguration(opts),
		checkOS(opts.Env.GOOS),
		checkArch(opts.Env.GOARCH),
		checkBinary(opts.Env, "git", StatusFail,
			"Git is required to check out repositories during a workflow job.",
			"sudo apt-get update && sudo apt-get install -y git"),
		checkBinary(opts.Env, "curl", StatusWarn,
			"Runnerly does not need curl, but many workflows assume it is present.",
			"sudo apt-get update && sudo apt-get install -y curl"),
	}

	dockerInstalled := checkDocker(opts)
	checks = append(checks, dockerInstalled, checkDockerDaemon(ctx, opts, dockerInstalled.Status))
	checks = append(checks,
		checkOutboundHTTPS(ctx, opts),
		checkGitHubAPI(ctx, opts),
		checkGitHubCredentials(ctx, opts),
		checkServer(ctx, opts),
	)

	return newReport(checks)
}

func newReport(checks []Check) Report {
	r := Report{Checks: checks}
	for _, c := range checks {
		switch c.Status {
		case StatusPass:
			r.Summary.Pass++
		case StatusWarn:
			r.Summary.Warn++
		case StatusFail:
			r.Summary.Fail++
		case StatusSkip:
			r.Summary.Skip++
		}
	}
	return r
}

func checkConfiguration(opts Options) Check {
	c := Check{Name: "configuration"}
	if err := config.Validate(opts.Config); err != nil {
		c.Status = StatusFail
		c.Detail = fmt.Sprintf("%s is not valid: %v", opts.ConfigPath, err)
		c.Remedy = "runnerly config edit"
		return c
	}
	c.Status = StatusPass
	if opts.ConfigFound {
		c.Detail = fmt.Sprintf("loaded %s", opts.ConfigPath)
	} else {
		c.Detail = fmt.Sprintf("no file at %s, using defaults", opts.ConfigPath)
	}
	return c
}

func checkOS(goos string) Check {
	c := Check{Name: "operating system"}
	if goos == "linux" {
		c.Status = StatusPass
		c.Detail = "Linux detected"
		return c
	}
	c.Status = StatusWarn
	c.Detail = fmt.Sprintf("%s detected. Runnerly manages runners on Linux only.\n"+
		"The CLI works here for development; do not register a runner from this machine.", goos)
	return c
}

// supportedArch maps Go architectures to the names GitHub uses for its runner
// release artifacts.
var supportedArch = map[string]string{
	"amd64": "x64",
	"arm64": "arm64",
}

func checkArch(goarch string) Check {
	c := Check{Name: "architecture"}
	if name, ok := supportedArch[goarch]; ok {
		c.Status = StatusPass
		c.Detail = fmt.Sprintf("%s detected (GitHub runner: linux-%s)", goarch, name)
		return c
	}
	c.Status = StatusFail
	c.Detail = fmt.Sprintf("%s is not an architecture GitHub publishes an Actions runner for", goarch)
	c.Remedy = "Use an x86_64 or arm64 machine."
	return c
}

func checkBinary(env Env, name string, missing Status, why, remedy string) Check {
	c := Check{Name: name}
	path, err := env.LookPath(name)
	if err != nil {
		c.Status = missing
		c.Detail = fmt.Sprintf("%s was not found on PATH. %s", name, why)
		c.Remedy = remedy
		return c
	}
	c.Status = StatusPass
	c.Detail = path
	return c
}

func checkDocker(opts Options) Check {
	if opts.Config.Executor.Type == config.ExecutorHost {
		return Check{
			Name:   "docker",
			Status: StatusSkip,
			Detail: "executor.type is host, so Docker is not required",
		}
	}
	return checkBinary(opts.Env, "docker", StatusFail,
		"executor.type is docker, so the Docker CLI is required.",
		"curl -fsSL https://get.docker.com | sh")
}

func checkDockerDaemon(ctx context.Context, opts Options, dockerStatus Status) Check {
	c := Check{Name: "docker daemon"}
	if dockerStatus != StatusPass {
		c.Status = StatusSkip
		c.Detail = "the Docker CLI is unavailable, so the daemon was not probed"
		return c
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	out, err := opts.Env.Run(ctx, "docker", "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		c.Status = StatusFail
		c.Detail = "Docker is installed but the daemon did not respond.\n" + firstLine(string(out))
		c.Remedy = "sudo systemctl start docker"
		return c
	}
	c.Status = StatusPass
	c.Detail = "server version " + firstLine(string(out))
	return c
}

func checkOutboundHTTPS(ctx context.Context, opts Options) Check {
	c := Check{Name: "outbound HTTPS"}
	if opts.Offline {
		return skipOffline(c)
	}
	addr := net.JoinHostPort(apiHost(opts.Config.GitHub.Host), "443")

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	conn, err := opts.Env.Dial(ctx, "tcp", addr)
	if err != nil {
		c.Status = StatusFail
		c.Detail = fmt.Sprintf("could not open a TCP connection to %s: %v", addr, err)
		c.Remedy = "Check the machine's firewall and proxy settings; runners need outbound port 443."
		return c
	}
	_ = conn.Close()
	c.Status = StatusPass
	c.Detail = "reached " + addr
	return c
}

func checkGitHubAPI(ctx context.Context, opts Options) Check {
	c := Check{Name: "GitHub API"}
	if opts.Offline {
		return skipOffline(c)
	}
	endpoint := github.APIBaseURL(opts.Config.GitHub.Host)
	status, err := get(ctx, opts, endpoint)
	if err != nil {
		c.Status = StatusFail
		c.Detail = fmt.Sprintf("%s did not respond: %v", endpoint, err)
		c.Remedy = "Check DNS and outbound HTTPS, then retry."
		return c
	}
	if status >= 500 {
		c.Status = StatusFail
		c.Detail = fmt.Sprintf("%s returned HTTP %d", endpoint, status)
		c.Remedy = "Check https://www.githubstatus.com and retry."
		return c
	}
	c.Status = StatusPass
	c.Detail = fmt.Sprintf("%s responded with HTTP %d", endpoint, status)
	return c
}

func checkGitHubCredentials(ctx context.Context, opts Options) Check {
	c := Check{Name: "GitHub credentials"}

	if opts.Token == "" {
		c.Status = StatusWarn
		c.Detail = "no GitHub token was found, so runners cannot be registered.\n" +
			"Everything else on this machine can still be checked."
		c.Remedy = "runnerly login"
		return c
	}
	if opts.Offline {
		return skipOffline(c)
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	client := github.NewForHost(opts.Config.GitHub.Host, opts.Token,
		github.WithHTTPClient(opts.Env.HTTPClient))

	identity, err := client.Identify(ctx)
	if err != nil {
		c.Status = StatusFail
		c.Detail = err.Error()
		if github.IsUnauthorized(err) {
			c.Remedy = "runnerly login"
		}
		return c
	}

	detail := fmt.Sprintf("authenticated as %s", identity.User.Login)
	if opts.TokenOrigin != "" {
		detail += fmt.Sprintf(" (token from %s)", opts.TokenOrigin)
	}

	// The scope the token needs depends on where runners are registered.
	kind := github.KindRepository
	if opts.Config.GitHub.Scope == config.ScopeOrganization {
		kind = github.KindOrganization
	}
	if missing := github.MissingScope(kind, identity.Scopes); missing != "" {
		c.Status = StatusWarn
		c.Detail = detail + fmt.Sprintf("\nThe token has no %q scope, which registering %s runners requires.", missing, kind)
		c.Remedy = "Create a token with the " + missing + " scope, then run `runnerly login`"
		return c
	}

	c.Status = StatusPass
	c.Detail = detail
	return c
}

func checkServer(ctx context.Context, opts Options) Check {
	c := Check{Name: "Runnerly server"}
	if opts.Config.Server.URL == "" {
		c.Status = StatusSkip
		c.Detail = "no control plane configured; the CLI does not require one"
		return c
	}
	if opts.Offline {
		return skipOffline(c)
	}
	endpoint := strings.TrimRight(opts.Config.Server.URL, "/") + "/api/v1/health"
	status, err := get(ctx, opts, endpoint)
	if err != nil {
		c.Status = StatusFail
		c.Detail = fmt.Sprintf("%s did not respond: %v", endpoint, err)
		c.Remedy = "Confirm server.url in the configuration and that runnerly-server is running."
		return c
	}
	if status != http.StatusOK {
		c.Status = StatusFail
		c.Detail = fmt.Sprintf("%s returned HTTP %d, want 200", endpoint, status)
		c.Remedy = "Check the Runnerly server logs."
		return c
	}
	c.Status = StatusPass
	c.Detail = "healthy at " + endpoint
	return c
}

func skipOffline(c Check) Check {
	c.Status = StatusSkip
	c.Detail = "skipped because --offline was set"
	return c
}

func get(ctx context.Context, opts Options, endpoint string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "runnerly-doctor")

	resp, err := opts.Env.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// apiHost returns the hostname github.APIBaseURL resolves to, for dial checks.
func apiHost(host string) string {
	u, err := url.Parse(github.APIBaseURL(host))
	if err != nil || u.Hostname() == "" {
		return "api.github.com"
	}
	return u.Hostname()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no output"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
