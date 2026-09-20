package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/ui"
)

// healthyEnv returns an Env where every external dependency is present and
// working. Individual tests break one thing at a time.
func healthyEnv(t *testing.T, serverURL string) Env {
	t.Helper()
	return Env{
		GOOS:   "linux",
		GOARCH: "amd64",
		LookPath: func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		},
		Run: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("27.0.1\n"), nil
		},
		HTTPClient: serverURL2Client(serverURL),
		Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		},
	}
}

// serverURL2Client returns a client that sends every request to the given test
// server, regardless of the host in the URL.
func serverURL2Client(base string) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			target, err := http.NewRequest(req.Method, base+req.URL.Path, nil) //nolint:noctx // test helper
			if err != nil {
				return nil, err
			}
			return http.DefaultTransport.RoundTrip(target.WithContext(req.Context()))
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// okServer answers every request with 200 and a body that also satisfies the
// credentials check, so a test only has to break the one thing it is about.
func okServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-OAuth-Scopes", "repo, workflow")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"octocat"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func byName(t *testing.T, r Report, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no check named %q in report", name)
	return Check{}
}

func TestRunHealthyMachinePasses(t *testing.T) {
	srv := okServer(t)
	report := Run(context.Background(), Options{
		Config: config.Default(),
		Token:  "ghp_test",
		Env:    healthyEnv(t, srv.URL),
	})

	if !report.OK() {
		t.Fatalf("healthy machine reported failures: %+v", report)
	}
	if report.Summary.Fail != 0 || report.Summary.Warn != 0 {
		t.Errorf("summary = %+v, want no failures or warnings", report.Summary)
	}
	// The control plane is optional and unset by default.
	if got := byName(t, report, "Runnerly server"); got.Status != StatusSkip {
		t.Errorf("Runnerly server = %q, want skip when server.url is empty", got.Status)
	}
}

func TestDockerDaemonDownFailsWithActionableRemedy(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	env.Run = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("Cannot connect to the Docker daemon at unix:///var/run/docker.sock.\n"), errors.New("exit status 1")
	}

	report := Run(context.Background(), Options{Config: config.Default(), Env: env})
	c := byName(t, report, "docker daemon")

	if c.Status != StatusFail {
		t.Fatalf("docker daemon = %q, want fail", c.Status)
	}
	if !strings.Contains(c.Detail, "Cannot connect to the Docker daemon") {
		t.Errorf("detail did not include the daemon error: %q", c.Detail)
	}
	if c.Remedy == "" {
		t.Error("a failing check must suggest an action")
	}
	if report.OK() {
		t.Error("report.OK() = true despite a failing check")
	}
}

func TestDockerMissingSkipsDaemonProbe(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	env.LookPath = func(file string) (string, error) {
		if file == "docker" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}

	report := Run(context.Background(), Options{Config: config.Default(), Env: env})

	if got := byName(t, report, "docker"); got.Status != StatusFail {
		t.Errorf("docker = %q, want fail when executor.type is docker", got.Status)
	}
	if got := byName(t, report, "docker daemon"); got.Status != StatusSkip {
		t.Errorf("docker daemon = %q, want skip when the CLI is missing", got.Status)
	}
}

func TestHostExecutorDoesNotRequireDocker(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	env.LookPath = func(file string) (string, error) {
		if file == "docker" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}
	cfg := config.Default()
	cfg.Executor.Type = config.ExecutorHost

	report := Run(context.Background(), Options{Config: cfg, Env: env})

	if got := byName(t, report, "docker"); got.Status != StatusSkip {
		t.Errorf("docker = %q, want skip for the host executor", got.Status)
	}
	if !report.OK() {
		t.Errorf("host executor without Docker should still pass: %+v", report.Summary)
	}
}

func TestMissingGitFailsButMissingCurlOnlyWarns(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	env.LookPath = func(file string) (string, error) {
		if file == "git" || file == "curl" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}

	report := Run(context.Background(), Options{Config: config.Default(), Env: env})

	if got := byName(t, report, "git"); got.Status != StatusFail {
		t.Errorf("git = %q, want fail", got.Status)
	}
	if got := byName(t, report, "curl"); got.Status != StatusWarn {
		t.Errorf("curl = %q, want warn", got.Status)
	}
}

func TestNonLinuxWarnsRatherThanFails(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	env.GOOS = "darwin"

	report := Run(context.Background(), Options{Config: config.Default(), Env: env})
	c := byName(t, report, "operating system")

	if c.Status != StatusWarn {
		t.Errorf("operating system = %q, want warn on darwin", c.Status)
	}
	if !report.OK() {
		t.Error("darwin should not fail the report; the CLI still works")
	}
}

func TestUnsupportedArchitectureFails(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	env.GOARCH = "riscv64"

	report := Run(context.Background(), Options{Config: config.Default(), Env: env})
	if got := byName(t, report, "architecture"); got.Status != StatusFail {
		t.Errorf("architecture = %q, want fail for riscv64", got.Status)
	}
}

func TestOfflineSkipsNetworkChecks(t *testing.T) {
	env := healthyEnv(t, "http://127.0.0.1:1")
	env.Dial = func(_ context.Context, _, _ string) (net.Conn, error) {
		t.Fatal("offline run dialed the network")
		return nil, nil
	}
	cfg := config.Default()
	cfg.Server.URL = "https://runnerly.internal"

	report := Run(context.Background(), Options{Config: cfg, Offline: true, Env: env})

	for _, name := range []string{"outbound HTTPS", "GitHub API", "Runnerly server"} {
		if got := byName(t, report, name); got.Status != StatusSkip {
			t.Errorf("%s = %q, want skip when offline", name, got.Status)
		}
	}
	if !report.OK() {
		t.Errorf("offline run should not fail: %+v", report.Summary)
	}
}

func TestConfiguredServerIsProbed(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Server.URL = srv.URL + "/"

	report := Run(context.Background(), Options{Config: cfg, Env: healthyEnv(t, srv.URL)})

	if got := byName(t, report, "Runnerly server"); got.Status != StatusPass {
		t.Errorf("Runnerly server = %q (%s), want pass", got.Status, got.Detail)
	}
	if gotPath != "/api/v1/health" {
		t.Errorf("probed %q, want /api/v1/health (trailing slash in server.url must not double up)", gotPath)
	}
}

func TestUnreachableServerFails(t *testing.T) {
	srv := okServer(t)
	cfg := config.Default()
	cfg.Server.URL = "https://runnerly.internal"

	env := healthyEnv(t, srv.URL)
	env.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		}),
	}

	report := Run(context.Background(), Options{Config: cfg, Env: env})
	c := byName(t, report, "Runnerly server")

	if c.Status != StatusFail {
		t.Fatalf("Runnerly server = %q, want fail", c.Status)
	}
	if c.Remedy == "" {
		t.Error("unreachable server must suggest an action")
	}
}

func TestGitHubAPIServerErrorFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	report := Run(context.Background(), Options{Config: config.Default(), Env: healthyEnv(t, srv.URL)})
	if got := byName(t, report, "GitHub API"); got.Status != StatusFail {
		t.Errorf("GitHub API = %q, want fail on HTTP 502", got.Status)
	}
}

func TestInvalidConfigurationFails(t *testing.T) {
	srv := okServer(t)
	cfg := config.Default()
	cfg.Executor.Type = "podman"

	report := Run(context.Background(), Options{
		Config:     cfg,
		ConfigPath: "/etc/runnerly/config.yaml",
		Env:        healthyEnv(t, srv.URL),
	})
	c := byName(t, report, "configuration")

	if c.Status != StatusFail {
		t.Fatalf("configuration = %q, want fail", c.Status)
	}
	if !strings.Contains(c.Detail, "/etc/runnerly/config.yaml") {
		t.Errorf("detail should name the offending file: %q", c.Detail)
	}
}

func TestRenderExplainsFailuresButNotPasses(t *testing.T) {
	var buf bytes.Buffer
	Render(ui.NewPlain(&buf), newReport([]Check{
		{Name: "git", Status: StatusPass, Detail: "/usr/bin/git"},
		{Name: "docker daemon", Status: StatusFail, Detail: "daemon is not running", Remedy: "sudo systemctl start docker"},
	}))
	out := buf.String()

	if strings.Contains(out, "/usr/bin/git") {
		t.Errorf("passing checks should stay quiet:\n%s", out)
	}
	for _, want := range []string{"daemon is not running", "sudo systemctl start docker", "1 check(s) failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderJSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	want := newReport([]Check{{Name: "git", Status: StatusPass}})
	if err := RenderJSON(&buf, want); err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if got.Summary.Pass != 1 || got.Checks[0].Name != "git" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestCredentialsCheckWarnsWithoutAToken(t *testing.T) {
	srv := okServer(t)
	report := Run(context.Background(), Options{Config: config.Default(), Env: healthyEnv(t, srv.URL)})
	c := byName(t, report, "GitHub credentials")

	if c.Status != StatusWarn {
		t.Fatalf("GitHub credentials = %q, want warn when no token is configured", c.Status)
	}
	if c.Remedy == "" {
		t.Error("the check must tell the operator how to get a token")
	}
	if !report.OK() {
		t.Error("a missing token must not fail the report; doctor works without one")
	}
}

func TestCredentialsCheckPasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.Header().Set("X-OAuth-Scopes", "repo, workflow")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	report := Run(context.Background(), Options{
		Config:      config.Default(),
		Token:       "ghp_test",
		TokenOrigin: "RUNNERLY_GITHUB_TOKEN",
		Env:         healthyEnv(t, srv.URL),
	})
	c := byName(t, report, "GitHub credentials")

	if c.Status != StatusPass {
		t.Fatalf("GitHub credentials = %q (%s), want pass", c.Status, c.Detail)
	}
	if !strings.Contains(c.Detail, "octocat") || !strings.Contains(c.Detail, "RUNNERLY_GITHUB_TOKEN") {
		t.Errorf("detail should name the account and the token source: %q", c.Detail)
	}
}

func TestCredentialsCheckWarnsOnMissingScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.Header().Set("X-OAuth-Scopes", "read:user")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"octocat"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	report := Run(context.Background(), Options{
		Config: config.Default(),
		Token:  "ghp_test",
		Env:    healthyEnv(t, srv.URL),
	})
	c := byName(t, report, "GitHub credentials")

	if c.Status != StatusWarn {
		t.Fatalf("GitHub credentials = %q, want warn for a token missing the repo scope", c.Status)
	}
	if !strings.Contains(c.Detail, "repo") {
		t.Errorf("detail should name the missing scope: %q", c.Detail)
	}
}

func TestCredentialsCheckFailsOnBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	report := Run(context.Background(), Options{
		Config: config.Default(),
		Token:  "ghp_revoked",
		Env:    healthyEnv(t, srv.URL),
	})
	c := byName(t, report, "GitHub credentials")

	if c.Status != StatusFail {
		t.Fatalf("GitHub credentials = %q, want fail for a revoked token", c.Status)
	}
	if c.Remedy != "runnerly login" {
		t.Errorf("Remedy = %q, want runnerly login", c.Remedy)
	}
	if report.OK() {
		t.Error("report.OK() = true despite an invalid token")
	}
}

func TestCredentialsCheckSkippedWhenOffline(t *testing.T) {
	env := healthyEnv(t, "http://127.0.0.1:1")
	report := Run(context.Background(), Options{
		Config:  config.Default(),
		Token:   "ghp_test",
		Offline: true,
		Env:     env,
	})
	if got := byName(t, report, "GitHub credentials"); got.Status != StatusSkip {
		t.Errorf("GitHub credentials = %q, want skip when offline", got.Status)
	}
}

func TestDockerDiskCheckSkippedWithoutADaemon(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	env.LookPath = func(file string) (string, error) {
		if file == "docker" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}

	report := Run(context.Background(), Options{Config: config.Default(), Token: "t", Env: env})
	if got := byName(t, report, "docker disk space"); got.Status != StatusSkip {
		t.Errorf("docker disk space = %q, want skip when there is no daemon", got.Status)
	}
}

func TestDockerDiskCheckReadsTheRealFilesystem(t *testing.T) {
	srv := okServer(t)
	env := healthyEnv(t, srv.URL)
	// Point Docker's root at a directory that certainly exists, so the
	// check exercises the real statfs rather than a fake number.
	root := t.TempDir()
	env.Run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "info" {
			for _, a := range args {
				if strings.Contains(a, "DockerRootDir") {
					return []byte(root + "\n"), nil
				}
			}
			return []byte("27.0.1\n"), nil
		}
		return []byte("27.0.1\n"), nil
	}

	report := Run(context.Background(), Options{Config: config.Default(), Token: "t", Env: env})
	c := byName(t, report, "docker disk space")

	if c.Status == StatusSkip {
		t.Skipf("free space is not readable on this platform: %s", c.Detail)
	}
	if !strings.Contains(c.Detail, "free of") {
		t.Errorf("detail should report free and total: %q", c.Detail)
	}
	// A developer machine running this test has more than 2 GiB free.
	if c.Status == StatusFail {
		t.Errorf("disk reported as critically low: %q", c.Detail)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := map[uint64]string{
		512:     "512 B",
		2048:    "2.0 KiB",
		5 << 20: "5.0 MiB",
		3 << 30: "3.0 GiB",
		2 << 40: "2.0 TiB",
	}
	for value, want := range tests {
		if got := humanBytes(value); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", value, got, want)
		}
	}
}
