package store

import (
	"context"
	"fmt"
	"time"
)

// runnerColumns is the select list every runner query shares, so scanRunner
// can be written once.
const runnerColumns = `
	id, name, github_host, github_scope, github_scope_id, github_runner_id,
	status, status_detail, os, architecture, cpu_count, memory_bytes, disk_bytes,
	labels, ephemeral, runner_version, agent_version,
	cpu_percent, memory_percent, disk_percent,
	last_heartbeat, created_at, updated_at`

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanRunner(row rowScanner) (Runner, error) {
	var r Runner
	err := row.Scan(
		&r.ID, &r.Name, &r.GitHubHost, &r.GitHubScope, &r.GitHubScopeID, &r.GitHubRunnerID,
		&r.Status, &r.StatusDetail, &r.OS, &r.Architecture, &r.CPUCount, &r.MemoryBytes, &r.DiskBytes,
		&r.Labels, &r.Ephemeral, &r.RunnerVersion, &r.AgentVersion,
		&r.CPUPercent, &r.MemoryPercent, &r.DiskPercent,
		&r.LastHeartbeat, &r.CreatedAt, &r.UpdatedAt,
	)
	return r, wrap(err)
}

// RegisterRunner describes a machine enrolling itself.
type RegisterRunner struct {
	Name          string
	GitHubHost    string
	GitHubScope   string
	GitHubScopeID string
	OS            string
	Architecture  string
	CPUCount      int
	MemoryBytes   int64
	DiskBytes     int64
	Labels        []string
	Ephemeral     bool
	RunnerVersion string
	AgentVersion  string
}

// UpsertRunner records a runner, or updates the existing one with the same
// host, scope and name.
//
// Enrollment is idempotent on purpose: a machine that is rebuilt, or an agent
// that restarts, should end up with the same runner row rather than a
// duplicate that an operator has to clean up.
func (s *Store) UpsertRunner(ctx context.Context, in RegisterRunner) (Runner, error) {
	if in.Name == "" {
		return Runner{}, fmt.Errorf("a runner needs a name")
	}
	if in.GitHubHost == "" {
		in.GitHubHost = "github.com"
	}
	if in.Labels == nil {
		in.Labels = []string{}
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO runners (
			id, name, github_host, github_scope, github_scope_id,
			status, os, architecture, cpu_count, memory_bytes, disk_bytes,
			labels, ephemeral, runner_version, agent_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (github_host, github_scope_id, name) DO UPDATE SET
			status        = EXCLUDED.status,
			os            = EXCLUDED.os,
			architecture  = EXCLUDED.architecture,
			cpu_count     = EXCLUDED.cpu_count,
			memory_bytes  = EXCLUDED.memory_bytes,
			disk_bytes    = EXCLUDED.disk_bytes,
			labels        = EXCLUDED.labels,
			ephemeral     = EXCLUDED.ephemeral,
			runner_version= EXCLUDED.runner_version,
			agent_version = EXCLUDED.agent_version,
			github_scope  = EXCLUDED.github_scope,
			updated_at    = now()
		RETURNING `+runnerColumns,
		newID(), in.Name, in.GitHubHost, in.GitHubScope, in.GitHubScopeID,
		StatusStarting, in.OS, in.Architecture, in.CPUCount, in.MemoryBytes, in.DiskBytes,
		in.Labels, in.Ephemeral, in.RunnerVersion, in.AgentVersion,
	)

	r, err := scanRunner(row)
	if err != nil {
		return Runner{}, fmt.Errorf("register runner %q: %w", in.Name, err)
	}
	return r, nil
}

// Heartbeat is one report from an agent.
type Heartbeat struct {
	Status        string
	StatusDetail  string
	CPUPercent    float64
	MemoryPercent float64
	DiskPercent   float64
	RunnerVersion string
	AgentVersion  string
}

// RecordHeartbeat stores an agent's report and stamps the time.
func (s *Store) RecordHeartbeat(ctx context.Context, runnerID string, hb Heartbeat) (Runner, error) {
	if hb.Status == "" {
		hb.Status = StatusOnline
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE runners SET
			status         = $2,
			status_detail  = $3,
			cpu_percent    = $4,
			memory_percent = $5,
			disk_percent   = $6,
			runner_version = COALESCE(NULLIF($7, ''), runner_version),
			agent_version  = COALESCE(NULLIF($8, ''), agent_version),
			last_heartbeat = now(),
			updated_at     = now()
		WHERE id = $1
		RETURNING `+runnerColumns,
		runnerID, hb.Status, hb.StatusDetail,
		hb.CPUPercent, hb.MemoryPercent, hb.DiskPercent,
		hb.RunnerVersion, hb.AgentVersion,
	)

	r, err := scanRunner(row)
	if err != nil {
		return Runner{}, fmt.Errorf("record heartbeat for %s: %w", runnerID, err)
	}
	return r, nil
}

// SetGitHubRunnerID records the id GitHub assigned, once it is known.
func (s *Store) SetGitHubRunnerID(ctx context.Context, runnerID string, githubID int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE runners SET github_runner_id = $2, updated_at = now() WHERE id = $1`,
		runnerID, githubID)
	if err != nil {
		return fmt.Errorf("set GitHub runner id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Runner returns one runner by id.
func (s *Store) Runner(ctx context.Context, id string) (Runner, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+runnerColumns+` FROM runners WHERE id = $1`, id)
	r, err := scanRunner(row)
	if err != nil {
		return Runner{}, fmt.Errorf("get runner %s: %w", id, err)
	}
	return r, nil
}

// ListRunnersOptions filters a listing.
type ListRunnersOptions struct {
	// Scope narrows to one repository or organization, as "owner/repo" or
	// "owner". Empty returns every runner.
	Scope string
	// Limit caps the result. Zero uses DefaultListLimit.
	Limit int
}

// DefaultListLimit bounds a listing so one request cannot pull the whole
// table into memory.
const DefaultListLimit = 200

// ListRunners returns runners, most recently created first.
func (s *Store) ListRunners(ctx context.Context, opts ListRunnersOptions) ([]Runner, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+runnerColumns+` FROM runners
		WHERE ($1 = '' OR github_scope_id = $1)
		ORDER BY created_at DESC
		LIMIT $2`, opts.Scope, limit)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	defer rows.Close()

	out := []Runner{}
	for rows.Next() {
		r, err := scanRunner(rows)
		if err != nil {
			return nil, fmt.Errorf("list runners: %w", err)
		}
		out = append(out, r)
	}
	return out, wrap(rows.Err())
}

// DeleteRunner removes a runner and everything that references it.
func (s *Store) DeleteRunner(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM runners WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete runner %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Counts summarizes the fleet for the dashboard's overview.
type Counts struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Busy    int `json:"busy"`
	Offline int `json:"offline"`
	Stale   int `json:"stale"`
	Error   int `json:"error"`
}

// CountRunners summarizes every runner, applying the freshness thresholds so
// a machine that stopped reporting is counted as offline.
func (s *Store) CountRunners(ctx context.Context, now time.Time, t Thresholds) (Counts, error) {
	runners, err := s.ListRunners(ctx, ListRunnersOptions{Limit: 10000})
	if err != nil {
		return Counts{}, err
	}

	var c Counts
	c.Total = len(runners)
	for _, r := range runners {
		if r.Health(now, t) == HealthStale {
			c.Stale++
		}
		switch r.EffectiveStatus(now, t) {
		case StatusOnline, StatusStarting:
			c.Online++
		case StatusBusy:
			c.Busy++
		case StatusError:
			c.Error++
		default:
			c.Offline++
		}
	}
	return c, nil
}
