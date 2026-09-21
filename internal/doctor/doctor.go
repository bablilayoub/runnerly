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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
		checkRunnerRuntime(ctx, opts),
		checkProtectedPaths(opts),
	}

	dockerInstalled := checkDocker(opts)
	daemon := checkDockerDaemon(ctx, opts, dockerInstalled.Status)
	checks = append(checks, dockerInstalled, daemon, checkDockerDisk(ctx, opts, daemon.Status))
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
	switch goos {
	case "darwin":
		// Runners register, run jobs, clean up and come back after a reboot
		// here. This used to warn that there was no service integration,
		// which was true until `agent launchd` existed.
		c.Status = StatusPass
		c.Detail = "macOS detected. Use `agent launchd` for the service; a LaunchAgent\n" +
			"needs the machine to log in, so enable automatic login on a build host."
		return c
	}

	c.Status = StatusWarn
	switch goos {
	case "windows":
		c.Detail = "Windows detected. Runnerly does not support Windows runners: the job\n" +
			"hooks it installs are shell scripts, so cleanup and busy reporting do not work."
	default:
		c.Detail = fmt.Sprintf("%s detected. Runnerly is developed and tested on Linux;\n"+
			"anything else is untried.", goos)
	}
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

// checkRunnerRuntime looks for the ICU libraries GitHub's runner needs.
//
// The runner is a .NET program, and .NET refuses to start without libicu
// unless it is built to avoid it. A clean Debian or Ubuntu machine does not
// have it, so `runner create` gets as far as downloading, unpacking and
// calling config.sh before failing with a message about "Dotnet Core 6.0"
// that says nothing about Runnerly:
//
//	Libicu's dependencies is missing for Dotnet Core 6.0
//
// That is a long way to go to find out, and it is the first thing a fresh
// Ubuntu box hits. Found by registering a runner in a bare ubuntu:24.04
// container, which is what a new machine actually looks like.
//
// It is a warning rather than a failure: the check reads the filesystem
// looking for a shared library, which is a guess, and being wrong should
// not stop a machine that works.
func checkRunnerRuntime(ctx context.Context, opts Options) Check {
	c := Check{Name: "runner runtime"}

	if opts.Env.GOOS != "linux" {
		c.Status = StatusSkip
		c.Detail = "only Linux needs the ICU libraries separately."
		return c
	}

	// ldconfig is the reliable answer where it exists; the glob is for
	// images that do not ship it.
	if out, err := opts.Env.Run(ctx, "ldconfig", "-p"); err == nil {
		if strings.Contains(string(out), "libicuuc.so") {
			c.Status = StatusPass
			c.Detail = "the ICU libraries GitHub's runner needs are present."
			return c
		}
	} else if found, globErr := filepath.Glob("/usr/lib/*/libicuuc.so*"); globErr == nil && len(found) > 0 {
		c.Status = StatusPass
		c.Detail = "the ICU libraries GitHub's runner needs are present."
		return c
	}

	c.Status = StatusWarn
	c.Detail = "the ICU libraries were not found. GitHub's runner is a .NET program\n" +
		"and config.sh fails without them, after the download has already happened."
	c.Remedy = "sudo apt-get update && sudo apt-get install -y libicu-dev\n" +
		"# or, on the unpacked runner: sudo ./bin/installdependencies.sh"
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

// Disk thresholds for the Docker executor. Images and layer cache are what
// fill a runner's disk, and a build that dies half way through a pull is a
// confusing way to find out.
const (
	diskFailBelow = 2 << 30  // 2 GiB
	diskWarnBelow = 10 << 30 // 10 GiB
)

func checkDockerDisk(ctx context.Context, opts Options, daemonStatus Status) Check {
	c := Check{Name: "docker disk space"}
	if daemonStatus != StatusPass {
		c.Status = StatusSkip
		c.Detail = "the Docker daemon is unavailable, so its storage was not checked"
		return c
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	out, err := opts.Env.Run(ctx, "docker", "info", "--format", "{{.DockerRootDir}}")
	root := firstLine(string(out))
	if err != nil || root == "" || root == "no output" {
		c.Status = StatusSkip
		c.Detail = "could not find where Docker stores its data"
		return c
	}

	free, total, err := diskFree(root)
	if err != nil {
		c.Status = StatusSkip
		c.Detail = fmt.Sprintf("could not read the free space on %s: %v", root, err)
		return c
	}

	detail := fmt.Sprintf("%s free of %s on %s", humanBytes(free), humanBytes(total), root)
	switch {
	case free < diskFailBelow:
		c.Status = StatusFail
		c.Detail = detail + "\nJobs will fail part way through pulling images."
		c.Remedy = "docker system prune --all --volumes"
	case free < diskWarnBelow:
		c.Status = StatusWarn
		c.Detail = detail + "\nImage layers accumulate quickly on a runner."
		c.Remedy = "docker system prune"
	default:
		c.Status = StatusPass
		c.Detail = detail
	}
	return c
}

// humanBytes renders a byte count for an operator.
func humanBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTP"[exp])
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

// tccProtected lists the folders macOS guards, relative to a home directory.
//
// A process launchd starts has no way to answer a consent prompt for one of
// these, so it does not fail — it blocks. See checkProtectedPaths.
var tccProtected = []string{
	"Desktop",
	"Documents",
	"Downloads",
	"Library/Mobile Documents", // iCloud Drive
}

// checkProtectedPaths warns when a path the agent needs sits in a folder
// macOS guards with a consent prompt.
//
// This is here because it happened. A LaunchAgent whose binary was on the
// Desktop came up with `launchctl print` reporting `state = running` and a
// pid, and never did anything: no log line, no error, no exit. The process
// was stopped inside dyld, in open(), on its own executable, waiting for a
// consent prompt that a background job cannot show and nobody was there to
// answer. Three minutes of that looks exactly like a hung agent.
//
// Nothing about the symptom points at the cause, which is what makes it
// worth a check. Moving the binary and the runner out of these folders is
// the whole fix; granting Full Disk Access to launchd is the other one, and
// is a much bigger hammer than a CI runner needs.
func checkProtectedPaths(opts Options) Check {
	c := Check{Name: "protected folders"}

	if opts.Env.GOOS != "darwin" {
		c.Status = StatusSkip
		c.Detail = "only macOS guards folders this way."
		return c
	}

	home, err := os.UserHomeDir()
	if err != nil {
		c.Status = StatusSkip
		c.Detail = "no home directory to check paths against."
		return c
	}

	// The runner directory comes from the configuration; the binary is
	// wherever this process was started from, which is what a generated
	// plist will name.
	candidates := map[string]string{
		"the runner directory": opts.Config.Runner.Dir,
	}
	if self, err := os.Executable(); err == nil {
		candidates["Runnerly itself"] = self
	}

	var found []string
	for what, path := range candidates {
		if path == "" {
			continue
		}
		if folder, ok := protectedFolder(home, path); ok {
			found = append(found, fmt.Sprintf("%s is in ~/%s", what, folder))
		}
	}

	if len(found) == 0 {
		c.Status = StatusPass
		c.Detail = "nothing the agent needs is in a folder macOS guards."
		return c
	}

	sort.Strings(found)
	c.Status = StatusWarn
	c.Detail = strings.Join(found, ", ") + ".\n" +
		"Run by launchd, a process reading one of these blocks on a consent prompt\n" +
		"it cannot show: the job reports itself running and does nothing at all."
	c.Remedy = "move them somewhere else, for example ~/.runnerly and /usr/local/bin"
	return c
}

// protectedFolder reports which guarded folder path is inside, if any.
func protectedFolder(home, path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	// Symlinks matter: /tmp is a link to /private/tmp, and a path through
	// one guarded folder into another is still guarded.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	realHome := home
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		realHome = resolved
	}

	for _, folder := range tccProtected {
		guarded := filepath.Join(realHome, folder)
		if abs == guarded || strings.HasPrefix(abs, guarded+string(filepath.Separator)) {
			return folder, true
		}
	}
	return "", false
}
