package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bablilayoub/runnerly/internal/github"
)

// Env is the outside world, injected so installation is testable without
// downloading a real 200 MB release or executing a real config.sh.
type Env struct {
	GOOS       string
	GOARCH     string
	HTTPClient *http.Client
	// Run executes a command inside dir and returns its combined output.
	Run func(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// DefaultEnv returns an Env wired to the real machine.
func DefaultEnv() Env {
	return Env{
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
		// Downloads are large; the timeout has to cover a slow link.
		HTTPClient: &http.Client{Timeout: 30 * time.Minute},
		Run: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			// #nosec G204 -- the command is a fixed script inside the runner
			// directory Runnerly created; the indirection exists for tests.
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir = dir
			return cmd.CombinedOutput()
		},
	}
}

// Options describes a runner to install.
type Options struct {
	// Dir is the directory the runner is installed into. It is created if
	// missing and must be empty or hold a previous install.
	Dir string
	// Name is the runner name as it appears in GitHub.
	Name string
	// Labels are the custom labels to register. Platform labels are added
	// automatically.
	Labels []string
	// URL is the browser-facing scope URL, for example
	// https://github.com/owner/repo.
	URL string
	// RegistrationToken is GitHub's short-lived token. It is never written to
	// disk by Runnerly; config.sh exchanges it for the runner's own
	// credentials.
	RegistrationToken string
	// Group is the runner group to join. Empty uses GitHub's default.
	Group string
	// Work is the runner's working directory name, relative to Dir.
	Work string
	// Ephemeral registers a runner that accepts one job and then deregisters.
	Ephemeral bool
	// Replace takes over an existing registration with the same name instead
	// of failing.
	Replace bool
	// Progress, when set, receives human-readable step descriptions.
	Progress func(string)
}

// Result describes what was installed.
type Result struct {
	Dir      string
	Platform Platform
	Filename string
	Labels   []string
}

// ErrAlreadyConfigured means the directory already holds a configured runner.
var ErrAlreadyConfigured = errors.New("this directory already holds a configured runner")

// Install downloads, verifies, unpacks and configures the official runner.
//
// It is not idempotent by accident: a directory that already holds a
// configured runner is refused unless Replace is set, because silently
// reconfiguring one would orphan its registration in GitHub.
func Install(ctx context.Context, env Env, client *github.Client, scope github.Scope, opts Options) (*Result, error) {
	platform, err := PlatformFor(env.GOOS, env.GOARCH)
	if err != nil {
		return nil, err
	}
	if opts.Name == "" {
		return nil, errors.New("the runner needs a name")
	}
	if opts.Dir == "" {
		return nil, errors.New("the runner needs an installation directory")
	}
	if opts.RegistrationToken == "" {
		return nil, errors.New("the runner needs a registration token")
	}

	if configured, err := IsConfigured(opts.Dir); err != nil {
		return nil, err
	} else if configured && !opts.Replace {
		return nil, fmt.Errorf("%s: %w.\n"+
			"Remove it with `runnerly runner remove %s` and delete the directory, or pass --replace",
			opts.Dir, ErrAlreadyConfigured, opts.Name)
	}

	progress(opts, "resolving the runner release GitHub expects")
	download, err := client.FindDownload(ctx, scope, platform.OS, platform.Arch)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(opts.Dir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", opts.Dir, err)
	}

	// Only download when the directory does not already hold an unpacked
	// runner, so --replace does not re-fetch 200 MB for a reconfiguration.
	if !hasRunnerScripts(opts.Dir) {
		archive := filepath.Join(opts.Dir, download.Filename)
		progress(opts, fmt.Sprintf("downloading %s", download.Filename))
		if err := fetch(ctx, env, download, archive); err != nil {
			return nil, err
		}

		progress(opts, "unpacking the runner")
		if err := Extract(archive, opts.Dir); err != nil {
			return nil, err
		}
		if err := os.Remove(archive); err != nil {
			return nil, fmt.Errorf("remove %s: %w", archive, err)
		}
	} else {
		progress(opts, "reusing the runner already unpacked in "+opts.Dir)
	}

	labels := MergeLabels(platform, opts.Labels)
	progress(opts, "registering with GitHub")
	if err := configure(ctx, env, opts, labels); err != nil {
		return nil, err
	}

	return &Result{
		Dir:      opts.Dir,
		Platform: platform,
		Filename: download.Filename,
		Labels:   labels,
	}, nil
}

// IsConfigured reports whether a directory holds a runner that has already
// been registered. config.sh writes .runner when registration succeeds.
func IsConfigured(dir string) (bool, error) {
	_, err := os.Stat(filepath.Join(dir, ".runner"))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("inspect %s: %w", dir, err)
	}
}

// hasRunnerScripts reports whether the runner archive is already unpacked.
func hasRunnerScripts(dir string) bool {
	for _, name := range []string{"config.sh", "config.cmd"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// fetch downloads the release and verifies its checksum before it is trusted.
// A file that fails verification is deleted rather than left to be executed.
func fetch(ctx context.Context, env Env, download *github.Download, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, download.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}

	resp, err := env.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", download.DownloadURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", download.DownloadURL, resp.StatusCode)
	}

	file, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // dest is inside the runner directory
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	digest := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, digest), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("download %s: %w", download.DownloadURL, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("write %s: %w", dest, closeErr)
	}

	got := hex.EncodeToString(digest.Sum(nil))
	want := strings.ToLower(strings.TrimSpace(download.SHA256Checksum))
	if want == "" {
		_ = os.Remove(dest)
		return fmt.Errorf("GitHub published no checksum for %s, so it cannot be verified", download.Filename)
	}
	if got != want {
		_ = os.Remove(dest)
		return fmt.Errorf("%s failed verification.\n  expected sha256 %s\n  got            %s\n"+
			"The download was deleted. Retry; if it fails again, the file may have been tampered with",
			download.Filename, want, got)
	}
	return nil
}

// MergeLabels combines the platform labels GitHub's runner would apply with
// the operator's own, preserving order and dropping duplicates.
func MergeLabels(platform Platform, custom []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range append(LabelsFor(platform), custom...) {
		key := strings.ToLower(strings.TrimSpace(l))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

// configure drives config.sh. The registration token is passed as an argument
// because that is the only interface config.sh offers; it is short-lived and
// single-use, which is why GitHub issues a fresh one per registration.
func configure(ctx context.Context, env Env, opts Options, labels []string) error {
	work := opts.Work
	if work == "" {
		work = "_work"
	}

	args := []string{
		"--unattended",
		"--url", opts.URL,
		"--token", opts.RegistrationToken,
		"--name", opts.Name,
		"--labels", strings.Join(labels, ","),
		"--work", work,
	}
	if opts.Group != "" {
		args = append(args, "--runnergroup", opts.Group)
	}
	if opts.Ephemeral {
		args = append(args, "--ephemeral")
	}
	if opts.Replace {
		args = append(args, "--replace")
	}

	script := "./config.sh"
	if env.GOOS == "windows" {
		script = "config.cmd"
	}

	out, err := env.Run(ctx, opts.Dir, script, args...)
	if err != nil {
		return fmt.Errorf("%s failed in %s: %w\n%s", script, opts.Dir, err, redactToken(string(out), opts.RegistrationToken))
	}
	return nil
}

// redactToken keeps a registration token out of an error message, which may be
// logged or pasted into an issue.
func redactToken(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
}

func progress(opts Options, msg string) {
	if opts.Progress != nil {
		opts.Progress(msg)
	}
}
