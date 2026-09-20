package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestGetUsesRuntimeValues(t *testing.T) {
	got := Get()
	if got.Go != runtime.Version() {
		t.Errorf("Go = %q, want %q", got.Go, runtime.Version())
	}
	if got.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", got.OS, runtime.GOOS)
	}
	if got.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", got.Arch, runtime.GOARCH)
	}
	if got.Version == "" {
		t.Error("Version is empty")
	}
}

func TestStringIncludesVersionAndTarget(t *testing.T) {
	i := Info{Version: "1.2.3", Commit: "abc", Date: "now", Go: "go1.24", OS: "linux", Arch: "arm64"}
	s := i.String()
	for _, want := range []string{"1.2.3", "abc", "linux/arm64"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
	if i.Short() != "1.2.3" {
		t.Errorf("Short() = %q, want 1.2.3", i.Short())
	}
}
