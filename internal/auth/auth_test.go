package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func TestPathSitsBesideConfig(t *testing.T) {
	config := filepath.Join("etc", "runnerly", "config.yaml")
	want := filepath.Join("etc", "runnerly", FileName)
	if got := Path(config); got != want {
		t.Errorf("Path(%q) = %q, want %q", config, got, want)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	creds, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if len(creds.Hosts) != 0 {
		t.Errorf("Hosts = %v, want empty", creds.Hosts)
	}
}

func TestStoreAndResolveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)
	want := Host{Token: "ghp_secret_value", User: "octocat", Scopes: []string{"repo"}}

	if err := Store(path, "github.com", want); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Windows has no permission bits. See TestStoredTokenIsNotWorldReadable
	// below for what does and does not hold there.
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("credentials permissions = %o, want 600", perm)
	}

	tok, err := Resolve(path, "github.com", "", noEnv)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if tok.Value != want.Token {
		t.Errorf("Value = %q", tok.Value)
	}
	if tok.Source != SourceFile {
		t.Errorf("Source = %q, want file", tok.Source)
	}
	if tok.User != "octocat" {
		t.Errorf("User = %q", tok.User)
	}
}

func TestStoreRecordsCreatedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Store(path, "github.com", Host{Token: "t"}); err != nil {
		t.Fatal(err)
	}
	creds, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Hosts["github.com"].CreatedAt.IsZero() {
		t.Error("CreatedAt was not recorded")
	}
}

func TestResolvePrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Store(path, "github.com", Host{Token: "from-file"}); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "RUNNERLY_GITHUB_TOKEN" {
			return "from-env"
		}
		return ""
	}

	// A flag beats everything.
	tok, err := Resolve(path, "github.com", "from-flag", env)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value != "from-flag" || tok.Source != SourceFlag {
		t.Errorf("flag did not win: %+v", tok)
	}

	// The environment beats the file.
	tok, err = Resolve(path, "github.com", "", env)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value != "from-env" || tok.Source != SourceEnvironment {
		t.Errorf("environment did not beat the file: %+v", tok)
	}
	if tok.Origin != "RUNNERLY_GITHUB_TOKEN" {
		t.Errorf("Origin = %q, want the variable name", tok.Origin)
	}
}

func TestResolvePrefersRunnerlyEnvVar(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN":          "generic",
		"RUNNERLY_GITHUB_TOKEN": "specific",
	}
	tok, err := Resolve(filepath.Join(t.TempDir(), FileName), "github.com", "",
		func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value != "specific" {
		t.Errorf("Value = %q; a stray GITHUB_TOKEN must not win", tok.Value)
	}
}

func TestResolveFallsBackToGitHubToken(t *testing.T) {
	env := map[string]string{"GITHUB_TOKEN": "generic"}
	tok, err := Resolve(filepath.Join(t.TempDir(), FileName), "github.com", "",
		func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value != "generic" || tok.Origin != "GITHUB_TOKEN" {
		t.Errorf("token = %+v", tok)
	}
}

func TestResolveWithoutAnyTokenIsActionable(t *testing.T) {
	_, err := Resolve(filepath.Join(t.TempDir(), FileName), "github.com", "", noEnv)
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("error = %v, want ErrNoToken", err)
	}
	for _, want := range []string{"runnerly login", "RUNNERLY_GITHUB_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestResolveIgnoresBlankEnvironmentValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Store(path, "github.com", Host{Token: "from-file"}); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "RUNNERLY_GITHUB_TOKEN" {
			return "   "
		}
		return ""
	}
	tok, err := Resolve(path, "github.com", "", env)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value != "from-file" {
		t.Errorf("a whitespace-only variable shadowed the file: %+v", tok)
	}
}

func TestHostNormalization(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Store(path, "https://GitHub.com/", Host{Token: "t"}); err != nil {
		t.Fatal(err)
	}
	tok, err := Resolve(path, "github.com", "", noEnv)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if tok.Value != "t" {
		t.Error("host was not normalized on store or lookup")
	}
}

func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Store(path, "github.com", Host{Token: "t"}); err != nil {
		t.Fatal(err)
	}

	removed, err := Delete(path, "github.com")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !removed {
		t.Error("removed = false for a stored host")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the credentials file should be removed once it holds nothing")
	}

	removed, err = Delete(path, "github.com")
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if removed {
		t.Error("removed = true when there was nothing to remove")
	}
}

func TestDeleteKeepsOtherHosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Store(path, "github.com", Host{Token: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Store(path, "github.acme.com", Host{Token: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Delete(path, "github.com"); err != nil {
		t.Fatal(err)
	}

	creds, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds.Hosts) != 1 || creds.Hosts["github.acme.com"].Token != "b" {
		t.Errorf("hosts = %+v", creds.Hosts)
	}
}

func TestReadToken(t *testing.T) {
	got, err := ReadToken(strings.NewReader("\n\n  ghp_abc123  \nignored\n"))
	if err != nil {
		t.Fatalf("ReadToken() error = %v", err)
	}
	if got != "ghp_abc123" {
		t.Errorf("ReadToken() = %q", got)
	}

	if _, err := ReadToken(strings.NewReader("   \n\n")); err == nil {
		t.Error("ReadToken() accepted empty input")
	}
}

func TestRedact(t *testing.T) {
	tests := map[string]string{
		"":                 "****",
		"short":            "****",
		"ghp_abcdefgh1234": "****1234",
	}
	for in, want := range tests {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStoredTokenIsNotWorldReadable asserts the property Runnerly claims
// about the file holding a GitHub token: nobody else on the machine can
// read it.
//
// It does not hold on Windows, and saying so here is deliberate. Go turns
// a mode into the read-only attribute and nothing else; what limits
// access is the ACL the file inherits from its directory, which Runnerly
// does not set. In practice that directory is inside the user's profile
// and is already private — but Runnerly is not the thing making it so,
// and a test that skipped quietly would let that read as covered. It is
// listed as a gap in docs/windows.md.
func TestStoredTokenIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Runnerly does not set an ACL on Windows; see docs/windows.md")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", FileName)
	if err := Store(path, "github.com", Host{Token: "t", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("credentials directory is %o, want no group or other access", perm)
	}
}
