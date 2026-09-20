// Package jobstate records what the runner is doing right now.
//
// GitHub's runner can call a script when a job starts and when it finishes
// (ACTIONS_RUNNER_HOOK_JOB_STARTED and _COMPLETED). Runnerly installs hooks
// that write this file, which gives it two things it could not otherwise
// know: when to clean up after a job, and whether the runner is busy.
//
// Before this, the agent could see that the runner process was alive but not
// whether it was running anything, so it reported "online" for a machine
// that was flat out. The status is now the truth rather than an inference.
package jobstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bablilayoub/runnerly/internal/docker"
)

// Dir is the directory Runnerly keeps its own files in, inside the runner's
// installation. It is dotted so it does not collide with anything the
// official runner unpacks.
const Dir = ".runnerly"

// FileName holds the current job.
const FileName = "job.json"

// Status is what the runner is doing.
type Status string

const (
	// StatusIdle means no job is running.
	StatusIdle Status = "idle"
	// StatusRunning means a job is in progress.
	StatusRunning Status = "running"
)

// State is the contents of the job file.
type State struct {
	Status Status `json:"status"`

	// Job identifies what is running, taken from the workflow environment.
	Repository string `json:"repository,omitempty"`
	Workflow   string `json:"workflow,omitempty"`
	Job        string `json:"job,omitempty"`
	RunID      string `json:"run_id,omitempty"`

	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`

	// Docker is what existed when the job started. Cleanup removes whatever
	// appeared since, rather than pruning by age and taking someone else's
	// container with it.
	Docker docker.Snapshot `json:"docker,omitzero"`
}

// Running reports whether a job is in progress.
func (s State) Running() bool { return s.Status == StatusRunning }

// RanAJob reports whether a job has started here at all, finished or not.
//
// An ephemeral runner uses this to tell a completed job from a crash: its
// run.sh exits 0 either way, so the exit code proves nothing.
func (s State) RanAJob() bool { return !s.StartedAt.IsZero() }

// FinishedAJob reports whether a job started and then finished.
func (s State) FinishedAJob() bool {
	return s.RanAJob() && !s.FinishedAt.IsZero() && s.Status == StatusIdle
}

// Duration is how long the job took, or zero if it is still running or never
// ran.
func (s State) Duration() time.Duration {
	if !s.FinishedAJob() {
		return 0
	}
	return s.FinishedAt.Sub(s.StartedAt)
}

// Describe summarizes the job for a log line or a dashboard.
//
// It describes a finished job as readily as a running one: an ephemeral
// runner reports what it did after the fact, and gating this on Running
// meant that summary came out blank.
func (s State) Describe() string {
	switch {
	case s.Repository != "" && s.Workflow != "":
		return s.Repository + " / " + s.Workflow
	case s.Repository != "":
		return s.Repository
	case s.Running() || s.RanAJob():
		return "a job"
	default:
		return ""
	}
}

// StateDir returns Runnerly's directory inside a runner installation.
func StateDir(runnerDir string) string { return filepath.Join(runnerDir, Dir) }

// Path returns the job file for a runner installation.
func Path(runnerDir string) string { return filepath.Join(StateDir(runnerDir), FileName) }

// Read returns the current state. A missing file means idle, which is what a
// runner that has never run a job looks like.
func Read(path string) (State, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the runner directory
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{Status: StatusIdle}, nil
		}
		return State{Status: StatusIdle}, fmt.Errorf("read %s: %w", path, err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		// A half-written or corrupt file must not make the agent think a job
		// is running for ever.
		return State{Status: StatusIdle}, fmt.Errorf("parse %s: %w", path, err)
	}
	if state.Status == "" {
		state.Status = StatusIdle
	}
	return state, nil
}

// Write replaces the job file.
//
// The write goes to a temporary file and is renamed into place, so the agent
// never reads a half-written file: it polls this at heartbeat time with no
// locking between the two processes.
func Write(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode job state: %w", err)
	}

	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", temp, err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
