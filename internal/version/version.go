// Package version exposes build information for every Runnerly binary.
//
// The values below are overridden at link time:
//
//	go build -ldflags "-X github.com/bablilayoub/runnerly/internal/version.Version=0.1.0"
package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is the semantic version of the build.
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the RFC3339 build timestamp.
	Date = "unknown"
)

// Info describes a build.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// Get returns the build information for the running binary.
func Get() Info {
	return Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

// Short returns just the version string, e.g. "0.1.0".
func (i Info) Short() string { return i.Version }

// String returns a human readable multi-line description of the build.
func (i Info) String() string {
	return fmt.Sprintf(
		"runnerly %s\n  commit  %s\n  built   %s\n  go      %s\n  target  %s/%s",
		i.Version, i.Commit, i.Date, i.Go, i.OS, i.Arch,
	)
}
