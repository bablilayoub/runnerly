// Package runner installs and configures GitHub's official Actions runner.
//
// Runnerly does not reimplement the runner. It downloads the release GitHub
// itself advertises, verifies the checksum GitHub publishes with it, unpacks
// it, and drives config.sh. Supervising the resulting process is the agent's
// job, not this package's.
package runner

import "fmt"

// Platform is GitHub's name for an operating system and architecture pair, as
// used in its runner download listing. It is not the same vocabulary as Go's
// GOOS/GOARCH.
type Platform struct {
	OS   string
	Arch string
}

func (p Platform) String() string { return p.OS + "/" + p.Arch }

// goosToGitHub maps Go's GOOS to GitHub's name.
var goosToGitHub = map[string]string{
	"linux":   "linux",
	"darwin":  "osx",
	"windows": "win",
}

// goarchToGitHub maps Go's GOARCH to GitHub's name.
var goarchToGitHub = map[string]string{
	"amd64": "x64",
	"arm64": "arm64",
	"arm":   "arm",
}

// PlatformFor converts a Go GOOS/GOARCH pair into GitHub's naming.
func PlatformFor(goos, goarch string) (Platform, error) {
	os, ok := goosToGitHub[goos]
	if !ok {
		return Platform{}, fmt.Errorf("GitHub publishes no Actions runner for %s", goos)
	}
	arch, ok := goarchToGitHub[goarch]
	if !ok {
		return Platform{}, fmt.Errorf("GitHub publishes no Actions runner for %s/%s", goos, goarch)
	}
	return Platform{OS: os, Arch: arch}, nil
}

// LabelsFor returns the labels GitHub's runner applies to itself on a
// platform. Runnerly sends them explicitly so the labels in the configuration
// file are the whole truth about a runner, rather than a partial list that
// GitHub silently extends.
func LabelsFor(p Platform) []string {
	os := p.OS
	if os == "osx" {
		os = "macOS"
	}
	return []string{"self-hosted", os, p.Arch}
}
