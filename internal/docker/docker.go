// Package docker prepares and tidies the Docker environment a job uses.
//
// It does not decide whether a job runs in a container: a workflow's own
// `container:` and `services:` keys do that, and GitHub's runner acts on
// them. What this package does is make sure the daemon is usable, and remove
// what a job leaves behind afterwards.
//
// Everything goes through the `docker` CLI rather than the Engine API. The
// SDK is a large dependency for what amounts to half a dozen commands, and
// the CLI is already required on the machine for the runner to do anything
// with containers at all.
package docker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// DefaultTimeout bounds one docker command.
const DefaultTimeout = 2 * time.Minute

// ErrUnavailable means the daemon could not be reached.
var ErrUnavailable = errors.New("the Docker daemon is not reachable")

// Env is the outside world, injected so the package is testable without a
// daemon.
type Env struct {
	// Run executes a docker command and returns its combined output.
	Run func(ctx context.Context, args ...string) ([]byte, error)
	// Host, when set, is passed as DOCKER_HOST.
	Host string
	// Timeout bounds each command. Zero uses DefaultTimeout.
	Timeout time.Duration
}

// DefaultEnv returns an Env that shells out to the real docker CLI.
func DefaultEnv(host string, timeout time.Duration) Env {
	return Env{
		Host:    host,
		Timeout: timeout,
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			// #nosec G204 -- arguments are built by this package from fixed
			// subcommands and ids read back from docker itself.
			cmd := exec.CommandContext(ctx, "docker", args...)
			if host != "" {
				cmd.Env = append(cmd.Environ(), "DOCKER_HOST="+host)
			}
			return cmd.CombinedOutput()
		},
	}
}

// Client talks to Docker.
type Client struct {
	env Env
}

// New returns a Client.
func New(env Env) *Client {
	if env.Timeout <= 0 {
		env.Timeout = DefaultTimeout
	}
	return &Client{env: env}
}

// run executes one docker command.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.env.Timeout)
	defer cancel()

	out, err := c.env.Run(ctx, args...)
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, firstLine(text))
	}
	return text, nil
}

// lines splits command output into non-empty trimmed lines.
func lines(out string) []string {
	var result []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			result = append(result, line)
		}
	}
	return result
}

func firstLine(s string) string {
	if s == "" {
		return "no output"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// Info describes the daemon.
type Info struct {
	ServerVersion string
	Driver        string
	// RootDir is where images and volumes live, for a disk check.
	RootDir string
}

// Info asks the daemon about itself, which is also the cheapest way to find
// out whether it is reachable at all.
func (c *Client) Info(ctx context.Context) (Info, error) {
	out, err := c.run(ctx, "info", "--format", "{{.ServerVersion}}\n{{.Driver}}\n{{.DockerRootDir}}")
	if err != nil {
		return Info{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	parts := lines(out)
	var info Info
	if len(parts) > 0 {
		info.ServerVersion = parts[0]
	}
	if len(parts) > 1 {
		info.Driver = parts[1]
	}
	if len(parts) > 2 {
		info.RootDir = parts[2]
	}
	return info, nil
}

// Snapshot is what existed in Docker at one moment.
//
// Cleanup works by diffing two of these rather than pruning by age or label.
// A self-hosted runner is often not the only thing on its machine, and
// `docker system prune` would take someone else's stopped container with it.
type Snapshot struct {
	Containers []string
	Volumes    []string
	Networks   []string
}

// Take records the containers, volumes and networks that exist now.
func (c *Client) Take(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	var err error

	if snap.Containers, err = c.ids(ctx, "ps", "--all", "--quiet", "--no-trunc"); err != nil {
		return Snapshot{}, err
	}
	if snap.Volumes, err = c.ids(ctx, "volume", "ls", "--quiet"); err != nil {
		return Snapshot{}, err
	}
	if snap.Networks, err = c.ids(ctx, "network", "ls", "--quiet", "--no-trunc"); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func (c *Client) ids(ctx context.Context, args ...string) ([]string, error) {
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	ids := lines(out)
	sort.Strings(ids)
	return ids, nil
}

// Added returns what is in next but not in base.
func Added(base, next Snapshot) Snapshot {
	return Snapshot{
		Containers: difference(next.Containers, base.Containers),
		Volumes:    difference(next.Volumes, base.Volumes),
		Networks:   difference(next.Networks, base.Networks),
	}
}

// Empty reports whether the snapshot holds nothing.
func (s Snapshot) Empty() bool {
	return len(s.Containers) == 0 && len(s.Volumes) == 0 && len(s.Networks) == 0
}

// Total counts everything in the snapshot.
func (s Snapshot) Total() int {
	return len(s.Containers) + len(s.Volumes) + len(s.Networks)
}

func difference(from, without []string) []string {
	if len(from) == 0 {
		return nil
	}
	exclude := make(map[string]bool, len(without))
	for _, id := range without {
		exclude[id] = true
	}

	var out []string
	for _, id := range from {
		if !exclude[id] {
			out = append(out, id)
		}
	}
	return out
}

// CleanupResult reports what was removed and what refused to go.
type CleanupResult struct {
	Containers int
	Volumes    int
	Networks   int
	Images     int
	// Problems are things that could not be removed. They are reported
	// rather than returned as an error: a leftover volume is worth telling
	// an operator about, but it must not fail a job that already passed.
	Problems []string
}

// Removed counts everything that went.
func (r CleanupResult) Removed() int {
	return r.Containers + r.Volumes + r.Networks + r.Images
}

// Remove deletes everything in the snapshot.
//
// Order matters: containers hold volumes and attach to networks, so they go
// first or the rest refuse.
func (c *Client) Remove(ctx context.Context, snap Snapshot, pruneImages bool) CleanupResult {
	var result CleanupResult

	for _, id := range snap.Containers {
		if _, err := c.run(ctx, "rm", "--force", "--volumes", id); err != nil {
			result.Problems = append(result.Problems, problem("container", id, err))
			continue
		}
		result.Containers++
	}

	for _, id := range snap.Volumes {
		if _, err := c.run(ctx, "volume", "rm", "--force", id); err != nil {
			result.Problems = append(result.Problems, problem("volume", id, err))
			continue
		}
		result.Volumes++
	}

	for _, id := range snap.Networks {
		if _, err := c.run(ctx, "network", "rm", id); err != nil {
			result.Problems = append(result.Problems, problem("network", id, err))
			continue
		}
		result.Networks++
	}

	if pruneImages {
		// Only dangling images: anything still tagged is cache worth keeping,
		// and re-pulling it is slower than the disk it frees is worth.
		if _, err := c.run(ctx, "image", "prune", "--force"); err != nil {
			result.Problems = append(result.Problems, "dangling images: "+err.Error())
		} else {
			result.Images++
		}
	}

	return result
}

func problem(kind, id string, err error) string {
	short := id
	if len(short) > 12 {
		short = short[:12]
	}
	return fmt.Sprintf("%s %s: %s", kind, short, firstLine(err.Error()))
}
