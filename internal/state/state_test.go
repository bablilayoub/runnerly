package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bablilayoub/runnerly/internal/github"
)

func repoScope(owner, repo string) github.Scope {
	return github.Scope{Kind: github.KindRepository, Owner: owner, Repo: repo}
}

func sample(name string) Runner {
	return Runner{
		Name:   name,
		Scope:  repoScope("acme", "widgets"),
		Host:   "github.com",
		Dir:    "/opt/runnerly/runners/" + name,
		Labels: []string{"self-hosted", "linux", "x64"},
	}
}

func TestPathSitsBesideConfig(t *testing.T) {
	if got := Path("/etc/runnerly/config.yaml"); got != "/etc/runnerly/runners.yaml" {
		t.Errorf("Path() = %q", got)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if len(f.Runners) != 0 {
		t.Errorf("Runners = %v, want empty", f.Runners)
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)
	want := sample("runnerly-01")

	if err := Put(path, want); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state permissions = %o, want 600", perm)
	}

	got, err := Get(path, "runnerly-01")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Scope.String() != "acme/widgets" || got.Dir != want.Dir {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if got.InstalledAt.IsZero() {
		t.Error("InstalledAt was not recorded")
	}
	if strings.Join(got.Labels, ",") != "self-hosted,linux,x64" {
		t.Errorf("Labels = %v", got.Labels)
	}
}

func TestPutNeedsAName(t *testing.T) {
	if err := Put(filepath.Join(t.TempDir(), FileName), Runner{}); err == nil {
		t.Error("Put() accepted a runner with no name")
	}
}

func TestPutReplacesAnExistingEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	first := sample("runnerly-01")
	if err := Put(path, first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.Dir = "/somewhere/else"
	second.InstalledAt = time.Now().UTC().Truncate(time.Second)
	if err := Put(path, second); err != nil {
		t.Fatal(err)
	}

	runners, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 1 {
		t.Fatalf("got %d runners, want 1", len(runners))
	}
	if runners[0].Dir != "/somewhere/else" {
		t.Errorf("Dir = %q, want the replacement", runners[0].Dir)
	}
}

func TestLookupIgnoresCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Put(path, sample("Runnerly-01")); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(path, "runnerly-01"); err != nil {
		t.Errorf("Get() = %v; GitHub treats runner names case-insensitively", err)
	}
}

func TestGetUnknownRunner(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Put(path, sample("runnerly-01")); err != nil {
		t.Fatal(err)
	}
	_, err := Get(path, "absent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := Put(path, sample("runnerly-01")); err != nil {
		t.Fatal(err)
	}

	removed, err := Delete(path, "RUNNERLY-01")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !removed {
		t.Error("removed = false for a recorded runner")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("the state file should be removed once it holds nothing")
	}

	removed, err = Delete(path, "runnerly-01")
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if removed {
		t.Error("removed = true when there was nothing to remove")
	}
}

func TestDeleteKeepsOtherRunners(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	for _, n := range []string{"a", "b"} {
		if err := Put(path, sample(n)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Delete(path, "a"); err != nil {
		t.Fatal(err)
	}

	runners, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 1 || runners[0].Name != "b" {
		t.Errorf("runners = %+v", runners)
	}
}

func TestListIsSortedByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	for _, n := range []string{"charlie", "alpha", "bravo"} {
		if err := Put(path, sample(n)); err != nil {
			t.Fatal(err)
		}
	}
	runners, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range runners {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "alpha,bravo,charlie" {
		t.Errorf("List() = %v, want sorted order", names)
	}
}

func TestOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	_, err := Only(path)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Only() on an empty machine = %v, want ErrNotFound", err)
	}
	if err != nil && !strings.Contains(err.Error(), "runner create") {
		t.Errorf("the error should say how to install one: %v", err)
	}

	if err := Put(path, sample("solo")); err != nil {
		t.Fatal(err)
	}
	only, err := Only(path)
	if err != nil {
		t.Fatalf("Only() error = %v", err)
	}
	if only.Name != "solo" {
		t.Errorf("Only() = %q", only.Name)
	}

	if err := Put(path, sample("second")); err != nil {
		t.Fatal(err)
	}
	_, err = Only(path)
	if err == nil {
		t.Fatal("Only() guessed between two runners")
	}
	for _, want := range []string{"solo", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should list the candidates, missing %q: %v", want, err)
		}
	}
}

func TestNameIsRecoveredFromTheMapKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	// A hand-edited file may omit the redundant name field.
	contents := "runners:\n  edited:\n    scope:\n      kind: repository\n      owner: acme\n      repo: widgets\n    dir: /tmp/edited\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Get(path, "edited")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Name != "edited" {
		t.Errorf("Name = %q, want it taken from the map key", got.Name)
	}
}

func TestOrganizationScopeRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	r := sample("org-runner")
	r.Scope = github.ForOrganization("acme")
	if err := Put(path, r); err != nil {
		t.Fatal(err)
	}
	got, err := Get(path, "org-runner")
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope.Kind != github.KindOrganization || got.Scope.String() != "acme" {
		t.Errorf("Scope = %+v", got.Scope)
	}
}
