package runner

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/github"
)

// entry is one file in a test archive.
type entry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	linkname string
}

// makeArchive builds a gzip-compressed tar in memory.
func makeArchive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		flag := e.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     mode,
			Size:     int64(len(e.body)),
			Typeflag: flag,
			Linkname: e.linkname,
		}
		if flag != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if flag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeArchive(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestPlatformFor(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{"linux", "amd64", "linux/x64", false},
		{"linux", "arm64", "linux/arm64", false},
		{"darwin", "arm64", "osx/arm64", false},
		{"windows", "amd64", "win/x64", false},
		{"plan9", "amd64", "", true},
		{"linux", "riscv64", "", true},
	}
	for _, tt := range tests {
		got, err := PlatformFor(tt.goos, tt.goarch)
		if tt.wantErr {
			if err == nil {
				t.Errorf("PlatformFor(%s, %s) = %v, want an error", tt.goos, tt.goarch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("PlatformFor(%s, %s) error = %v", tt.goos, tt.goarch, err)
			continue
		}
		if got.String() != tt.want {
			t.Errorf("PlatformFor(%s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestLabelsFor(t *testing.T) {
	if got := strings.Join(LabelsFor(Platform{OS: "linux", Arch: "x64"}), ","); got != "self-hosted,linux,x64" {
		t.Errorf("LabelsFor(linux/x64) = %q", got)
	}
	if got := strings.Join(LabelsFor(Platform{OS: "osx", Arch: "arm64"}), ","); got != "self-hosted,macOS,arm64" {
		t.Errorf("LabelsFor(osx/arm64) = %q", got)
	}
}

func TestMergeLabelsDedupesCaseInsensitively(t *testing.T) {
	got := MergeLabels(Platform{OS: "linux", Arch: "x64"}, []string{"Linux", "docker", "  ", "docker", "runnerly"})
	want := "self-hosted,linux,x64,docker,runnerly"
	if strings.Join(got, ",") != want {
		t.Errorf("MergeLabels() = %v, want %s", got, want)
	}
}

func TestExtract(t *testing.T) {
	data := makeArchive(t,
		entry{name: "config.sh", body: "#!/bin/sh\n", mode: 0o755},
		entry{name: "bin/Runner.Listener", body: "binary", mode: 0o755},
		entry{name: "docs/", typeflag: tar.TypeDir, mode: 0o755},
		entry{name: "docs/readme.txt", body: "hello", mode: 0o644},
		entry{name: "bin/link.sh", typeflag: tar.TypeSymlink, linkname: "Runner.Listener"},
	)
	dest := t.TempDir()

	if err := Extract(writeArchive(t, data), dest); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dest, "docs", "readme.txt"))
	if err != nil || string(body) != "hello" {
		t.Errorf("nested file = %q, %v", body, err)
	}

	info, err := os.Stat(filepath.Join(dest, "config.sh"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no executable bit. Go synthesizes a mode from the
	// read-only attribute, so everything writable reads as 0666 and this
	// would be asserting that Windows is unix.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("config.sh is not executable: %o", info.Mode().Perm())
	}

	link, err := os.Readlink(filepath.Join(dest, "bin", "link.sh"))
	if err != nil || link != "Runner.Listener" {
		t.Errorf("symlink = %q, %v", link, err)
	}
}

func TestExtractRejectsPathTraversal(t *testing.T) {
	cases := []struct {
		name    string
		entries []entry
		want    string
	}{
		{
			name:    "parent directory",
			entries: []entry{{name: "../escaped.sh", body: "x"}},
			want:    "outside",
		},
		{
			name:    "absolute path",
			entries: []entry{{name: "/etc/cron.d/backdoor", body: "x"}},
			want:    "absolute path",
		},
		{
			name:    "nested traversal",
			entries: []entry{{name: "bin/../../escaped", body: "x"}},
			want:    "outside",
		},
		{
			name:    "symlink escaping the directory",
			entries: []entry{{name: "evil", typeflag: tar.TypeSymlink, linkname: "../../../../etc/passwd"}},
			want:    "outside",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			err := Extract(writeArchive(t, makeArchive(t, tt.entries...)), dest)
			if err == nil {
				t.Fatal("Extract() accepted an entry that escapes the destination")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestExtractStripsGroupAndOtherWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits; a file's protection is its ACL")
	}
	dest := t.TempDir()
	data := makeArchive(t, entry{name: "loose.sh", body: "x", mode: 0o777})
	if err := Extract(writeArchive(t, data), dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "loose.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Errorf("mode = %o, want no group or other write", info.Mode().Perm())
	}
}

func TestExtractRejectsNonGzip(t *testing.T) {
	path := writeArchive(t, []byte("not a gzip stream"))
	if err := Extract(path, t.TempDir()); err == nil {
		t.Fatal("Extract() accepted a file that is not gzip")
	}
}

// installEnv wires an Env whose config.sh is a recorded fake.
func installEnv(t *testing.T, client *http.Client, record *[]string) Env {
	t.Helper()
	return Env{
		GOOS:       "linux",
		GOARCH:     "amd64",
		HTTPClient: client,
		Run: func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
			*record = append(*record, name+" "+strings.Join(args, " "))
			// config.sh writes .runner when registration succeeds.
			if err := os.WriteFile(filepath.Join(dir, ".runner"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			return []byte("Runner successfully added"), nil
		},
	}
}

// fakeGitHub serves the downloads endpoint and the archive itself.
func fakeGitHub(t *testing.T, archive []byte, checksum string) (*github.Client, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/actions/runners/downloads") {
			downloads := []github.Download{{
				OS:             "linux",
				Architecture:   "x64",
				Filename:       "actions-runner-linux-x64.tar.gz",
				DownloadURL:    "http://" + r.Host + "/archive.tar.gz",
				SHA256Checksum: checksum,
			}}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(downloads); err != nil {
				t.Fatal(err)
			}
			return
		}
		if r.URL.Path == "/archive.tar.gz" {
			_, _ = w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	return github.New("t", github.WithBaseURL(srv.URL), github.WithHTTPClient(srv.Client())), srv.Client()
}

func runnerArchive(t *testing.T) []byte {
	t.Helper()
	return makeArchive(t,
		entry{name: "config.sh", body: "#!/bin/sh\n", mode: 0o755},
		entry{name: "run.sh", body: "#!/bin/sh\n", mode: 0o755},
	)
}

func TestInstall(t *testing.T) {
	archive := runnerArchive(t)
	client, httpClient := fakeGitHub(t, archive, sum(archive))

	var commands []string
	dir := filepath.Join(t.TempDir(), "runnerly-01")
	var steps []string

	result, err := Install(context.Background(), installEnv(t, httpClient, &commands), client,
		github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Options{
			Dir:               dir,
			Name:              "runnerly-01",
			Labels:            []string{"docker"},
			URL:               "https://github.com/acme/widgets",
			RegistrationToken: "AREGTOKEN",
			Progress:          func(s string) { steps = append(steps, s) },
		})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if result.Platform.String() != "linux/x64" {
		t.Errorf("Platform = %q", result.Platform)
	}
	if strings.Join(result.Labels, ",") != "self-hosted,linux,x64,docker" {
		t.Errorf("Labels = %v", result.Labels)
	}

	// The archive is unpacked and then removed.
	if _, err := os.Stat(filepath.Join(dir, "config.sh")); err != nil {
		t.Errorf("config.sh was not unpacked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "actions-runner-linux-x64.tar.gz")); !os.IsNotExist(err) {
		t.Error("the downloaded archive was left behind")
	}

	if len(commands) != 1 {
		t.Fatalf("ran %d commands, want 1: %v", len(commands), commands)
	}
	for _, want := range []string{
		"./config.sh", "--unattended",
		"--url https://github.com/acme/widgets",
		"--token AREGTOKEN",
		"--name runnerly-01",
		"--labels self-hosted,linux,x64,docker",
		"--work _work",
	} {
		if !strings.Contains(commands[0], want) {
			t.Errorf("config.sh invocation missing %q:\n%s", want, commands[0])
		}
	}
	if len(steps) == 0 {
		t.Error("no progress was reported")
	}
}

func TestInstallRejectsATamperedDownload(t *testing.T) {
	archive := runnerArchive(t)
	client, httpClient := fakeGitHub(t, archive, sum([]byte("a different file")))

	var commands []string
	dir := filepath.Join(t.TempDir(), "runnerly-01")

	_, err := Install(context.Background(), installEnv(t, httpClient, &commands), client,
		github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Options{Dir: dir, Name: "r", URL: "https://github.com/acme/widgets", RegistrationToken: "T"})
	if err == nil {
		t.Fatal("Install() accepted an archive whose checksum did not match")
	}
	if !strings.Contains(err.Error(), "failed verification") {
		t.Errorf("error = %v", err)
	}
	if len(commands) != 0 {
		t.Error("config.sh ran despite a failed checksum")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "actions-runner-linux-x64.tar.gz")); !os.IsNotExist(statErr) {
		t.Error("an unverified download was left on disk")
	}
}

func TestInstallRejectsAMissingChecksum(t *testing.T) {
	archive := runnerArchive(t)
	client, httpClient := fakeGitHub(t, archive, "")

	var commands []string
	_, err := Install(context.Background(), installEnv(t, httpClient, &commands), client,
		github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Options{Dir: filepath.Join(t.TempDir(), "r"), Name: "r", URL: "https://x", RegistrationToken: "T"})
	if err == nil || !strings.Contains(err.Error(), "no checksum") {
		t.Errorf("error = %v, want a refusal to install an unverifiable download", err)
	}
}

func TestInstallRefusesToReconfigureWithoutReplace(t *testing.T) {
	archive := runnerArchive(t)
	client, httpClient := fakeGitHub(t, archive, sum(archive))

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".runner"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []string
	_, err := Install(context.Background(), installEnv(t, httpClient, &commands), client,
		github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Options{Dir: dir, Name: "runnerly-01", URL: "https://x", RegistrationToken: "T"})

	if !errors.Is(err, ErrAlreadyConfigured) {
		t.Fatalf("error = %v, want ErrAlreadyConfigured", err)
	}
	if !strings.Contains(err.Error(), "--replace") {
		t.Errorf("error should offer --replace, got: %v", err)
	}
}

func TestInstallWithReplaceReusesAnUnpackedRunner(t *testing.T) {
	archive := runnerArchive(t)
	client, httpClient := fakeGitHub(t, archive, sum(archive))

	dir := t.TempDir()
	// A previous install: scripts present and registered.
	if err := os.WriteFile(filepath.Join(dir, "config.sh"), []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".runner"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands []string
	var steps []string
	_, err := Install(context.Background(), installEnv(t, httpClient, &commands), client,
		github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Options{
			Dir: dir, Name: "runnerly-01", URL: "https://x", RegistrationToken: "T",
			Replace:  true,
			Progress: func(s string) { steps = append(steps, s) },
		})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !strings.Contains(commands[0], "--replace") {
		t.Errorf("config.sh was not told to replace: %s", commands[0])
	}
	if !strings.Contains(strings.Join(steps, "\n"), "reusing") {
		t.Errorf("a re-registration should not re-download: %v", steps)
	}
}

func TestInstallPassesEphemeralAndGroup(t *testing.T) {
	archive := runnerArchive(t)
	client, httpClient := fakeGitHub(t, archive, sum(archive))

	var commands []string
	_, err := Install(context.Background(), installEnv(t, httpClient, &commands), client,
		github.Scope{Kind: github.KindOrganization, Owner: "acme"},
		Options{
			Dir: filepath.Join(t.TempDir(), "r"), Name: "r", URL: "https://x",
			RegistrationToken: "T", Ephemeral: true, Group: "builders", Work: "_jobs",
		})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for _, want := range []string{"--ephemeral", "--runnergroup builders", "--work _jobs"} {
		if !strings.Contains(commands[0], want) {
			t.Errorf("invocation missing %q:\n%s", want, commands[0])
		}
	}
}

func TestInstallRedactsTheTokenFromAFailure(t *testing.T) {
	archive := runnerArchive(t)
	client, httpClient := fakeGitHub(t, archive, sum(archive))

	env := Env{
		GOOS: "linux", GOARCH: "amd64", HTTPClient: httpClient,
		Run: func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return []byte("failed while using token SUPERSECRET"), errors.New("exit status 1")
		},
	}

	_, err := Install(context.Background(), env, client,
		github.Scope{Kind: github.KindRepository, Owner: "acme", Repo: "widgets"},
		Options{Dir: filepath.Join(t.TempDir(), "r"), Name: "r", URL: "https://x", RegistrationToken: "SUPERSECRET"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("the registration token leaked into the error:\n%v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("error = %v, want the token redacted", err)
	}
}

func TestInstallValidatesItsInputs(t *testing.T) {
	client, httpClient := fakeGitHub(t, nil, "")
	env := Env{GOOS: "linux", GOARCH: "amd64", HTTPClient: httpClient}
	scope := github.Scope{Kind: github.KindRepository, Owner: "a", Repo: "b"}

	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"no name", Options{Dir: "/tmp/x", RegistrationToken: "T"}, "needs a name"},
		{"no directory", Options{Name: "r", RegistrationToken: "T"}, "installation directory"},
		{"no token", Options{Name: "r", Dir: "/tmp/x"}, "registration token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Install(context.Background(), env, client, scope, tt.opts); err == nil ||
				!strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestInstallOnAnUnsupportedPlatform(t *testing.T) {
	client, httpClient := fakeGitHub(t, nil, "")
	env := Env{GOOS: "plan9", GOARCH: "amd64", HTTPClient: httpClient}
	_, err := Install(context.Background(), env, client,
		github.Scope{Kind: github.KindRepository, Owner: "a", Repo: "b"},
		Options{Dir: "/tmp/x", Name: "r", RegistrationToken: "T"})
	if err == nil || !strings.Contains(err.Error(), "plan9") {
		t.Errorf("error = %v", err)
	}
}

func TestIsConfigured(t *testing.T) {
	dir := t.TempDir()
	configured, err := IsConfigured(dir)
	if err != nil || configured {
		t.Errorf("IsConfigured() = %v, %v for an empty directory", configured, err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".runner"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	configured, err = IsConfigured(dir)
	if err != nil || !configured {
		t.Errorf("IsConfigured() = %v, %v after registration", configured, err)
	}
}

func TestFetchReportsAnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "archive.tar.gz")
	err := fetch(context.Background(), Env{HTTPClient: srv.Client()},
		&github.Download{DownloadURL: srv.URL, Filename: "x.tar.gz", SHA256Checksum: "abc"}, dest)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(http.StatusForbidden)) {
		t.Errorf("error = %v, want the HTTP status", err)
	}
}

// TestDefaultLabelsDoNotDuplicatePlatform pins the two places labels come
// from together. The configuration's defaults used to name the OS with Go's
// spelling while the platform labels used GitHub's, so a Mac registered both
// "darwin" and "macOS". Nothing failed; the runner just carried a label that
// no workflow would ever ask for.
func TestDefaultLabelsDoNotDuplicatePlatform(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		want         []string
	}{
		{"linux", "amd64", []string{"self-hosted", "linux", "x64", "runnerly"}},
		{"linux", "arm64", []string{"self-hosted", "linux", "arm64", "runnerly"}},
		{"darwin", "arm64", []string{"self-hosted", "macOS", "arm64", "runnerly"}},
	} {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			platform, err := PlatformFor(tc.goos, tc.goarch)
			if err != nil {
				t.Fatalf("PlatformFor: %v", err)
			}
			got := MergeLabels(platform, config.Default().Runner.Labels)
			if !slices.Equal(got, tc.want) {
				t.Errorf("labels = %v, want %v", got, tc.want)
			}
		})
	}
}

// makeZip builds a zip archive in memory, the shape GitHub ships for
// Windows.
func makeZip(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := os.FileMode(e.mode)
		if strings.HasSuffix(e.name, "/") {
			mode |= os.ModeDir
		}
		if e.typeflag == tar.TypeSymlink {
			mode |= os.ModeSymlink
		}
		header.SetMode(mode)

		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		body := e.body
		if e.typeflag == tar.TypeSymlink {
			body = e.linkname
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeZip(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "actions-runner-win-x64-2.337.0.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// GitHub ships a zip for Windows and a tar.gz for everything else, so
// Extract has to read both. Which one it is comes from the name GitHub
// gave the file, not from the platform doing the unpacking: a release is
// downloaded by name, and the two have to agree.
func TestExtractReadsAZip(t *testing.T) {
	data := makeZip(t,
		entry{name: "config.cmd", body: "@echo off\n", mode: 0o755},
		entry{name: "bin/", mode: 0o755},
		entry{name: "bin/Runner.Listener.exe", body: "binary", mode: 0o755},
		entry{name: "docs/readme.txt", body: "hello", mode: 0o644},
	)
	dest := t.TempDir()

	if err := Extract(writeZip(t, data), dest); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dest, "docs", "readme.txt"))
	if err != nil || string(body) != "hello" {
		t.Errorf("nested file = %q, %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "Runner.Listener.exe")); err != nil {
		t.Errorf("the runner binary is missing: %v", err)
	}
}

// The traversal guard is the same one, but a zip reaches it by a
// different path, and "it is shared code" is how a hole gets left open.
func TestExtractZipRejectsPathTraversal(t *testing.T) {
	cases := []struct {
		name    string
		entries []entry
		want    string
	}{
		{"parent directory", []entry{{name: "../escaped.cmd", body: "x"}}, "outside"},
		{"absolute path", []entry{{name: "/Windows/System32/evil.dll", body: "x"}}, "absolute path"},
		{"nested traversal", []entry{{name: "bin/../../escaped", body: "x"}}, "outside"},
		{
			"symlink",
			[]entry{{name: "evil", typeflag: tar.TypeSymlink, linkname: "../../../../etc/passwd"}},
			"symlink",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			err := Extract(writeZip(t, makeZip(t, tt.entries...)), dest)
			if err == nil {
				t.Fatal("Extract() accepted an entry that escapes the destination")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// A zip written by a Windows tool records no unix mode at all. Taking it
// literally would create files with mode 0.
func TestExtractZipWithNoRecordedModes(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("config.cmd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("@echo off\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := Extract(writeZip(t, buf.Bytes()), dest); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "config.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o400 == 0 {
		t.Errorf("mode = %o, which nobody can read", info.Mode().Perm())
	}
}

func TestExtractZipStripsGroupAndOtherWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no permission bits; a file's protection is its ACL")
	}
	dest := t.TempDir()
	if err := Extract(writeZip(t, makeZip(t, entry{name: "loose.cmd", body: "x", mode: 0o777})), dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "loose.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Errorf("mode = %o, want no group or other write", info.Mode().Perm())
	}
}

func TestExtractRejectsAZipThatIsNotOne(t *testing.T) {
	if err := Extract(writeZip(t, []byte("not a zip")), t.TempDir()); err == nil {
		t.Fatal("Extract() accepted a .zip that is not one")
	}
}
