package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Commands an agent can be asked to carry out.
const (
	// CommandRestart asks the agent to stop the runner and start it again.
	CommandRestart = "restart"
)

// Command statuses.
const (
	// CommandPending means it has not reached the agent yet.
	CommandPending = "pending"
	// CommandDelivered means the agent has it but has not reported back.
	CommandDelivered = "delivered"
	// CommandDone means the agent carried it out.
	CommandDone = "done"
	// CommandFailed means the agent tried and could not.
	CommandFailed = "failed"
)

// Command is one queued instruction for an agent.
type Command struct {
	ID          string     `json:"id"`
	RunnerID    string     `json:"runner_id"`
	Command     string     `json:"command"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	RequestedBy string     `json:"requested_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

const commandColumns = `id, runner_id, command, status, error, requested_by,
	created_at, delivered_at, completed_at`

func scanCommand(row rowScanner) (Command, error) {
	var c Command
	err := row.Scan(&c.ID, &c.RunnerID, &c.Command, &c.Status, &c.Error, &c.RequestedBy,
		&c.CreatedAt, &c.DeliveredAt, &c.CompletedAt)
	return c, wrap(err)
}

// QueueCommand asks a runner's agent to do something on its next heartbeat.
//
// An identical command already waiting is returned instead of queuing a
// second one: a dashboard button pressed twice should not restart a runner
// twice.
func (s *Store) QueueCommand(ctx context.Context, runnerID, command, requestedBy string) (Command, error) {
	if command != CommandRestart {
		return Command{}, fmt.Errorf("%q is not a command an agent understands", command)
	}

	existing, err := s.pendingCommand(ctx, runnerID, command)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, ErrNotFound):
		return Command{}, fmt.Errorf("look for a pending %s: %w", command, err)
	}

	row := s.pool.QueryRow(ctx, `
		INSERT INTO runner_commands (id, runner_id, command, requested_by)
		VALUES ($1, $2, $3, $4)
		RETURNING `+commandColumns,
		newID(), runnerID, command, requestedBy)

	c, err := scanCommand(row)
	if err != nil {
		return Command{}, fmt.Errorf("queue %s for runner %s: %w", command, runnerID, err)
	}
	return c, nil
}

func (s *Store) pendingCommand(ctx context.Context, runnerID, command string) (Command, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+commandColumns+` FROM runner_commands
		WHERE runner_id = $1 AND command = $2 AND status IN ('pending', 'delivered')
		ORDER BY created_at LIMIT 1`, runnerID, command)
	return scanCommand(row)
}

// TakeCommands hands an agent everything waiting for it and marks it
// delivered.
//
// Delivery is recorded rather than the rows being deleted, so an operator can
// see that a command reached the machine even if the agent then died before
// carrying it out.
func (s *Store) TakeCommands(ctx context.Context, runnerID string) ([]Command, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE runner_commands SET status = 'delivered', delivered_at = now()
		WHERE id IN (
			SELECT id FROM runner_commands
			WHERE runner_id = $1 AND status = 'pending'
			ORDER BY created_at
			LIMIT 10
		)
		RETURNING `+commandColumns, runnerID)
	if err != nil {
		return nil, fmt.Errorf("take commands for runner %s: %w", runnerID, err)
	}
	defer rows.Close()

	out := []Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, fmt.Errorf("take commands for runner %s: %w", runnerID, err)
		}
		out = append(out, c)
	}
	return out, wrap(rows.Err())
}

// CompleteCommand records the outcome an agent reported.
//
// The runner id is part of the condition so one agent cannot close another's
// command by guessing an id.
func (s *Store) CompleteCommand(ctx context.Context, runnerID, commandID string, failure string) error {
	status := CommandDone
	if failure != "" {
		status = CommandFailed
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE runner_commands
		SET status = $3, error = $4, completed_at = now()
		WHERE id = $2 AND runner_id = $1 AND status IN ('pending', 'delivered')`,
		runnerID, commandID, status, failure)
	if err != nil {
		return fmt.Errorf("complete command %s: %w", commandID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListCommands returns a runner's recent commands, newest first.
func (s *Store) ListCommands(ctx context.Context, runnerID string, limit int) ([]Command, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+commandColumns+` FROM runner_commands
		WHERE runner_id = $1 ORDER BY created_at DESC LIMIT $2`, runnerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list commands for runner %s: %w", runnerID, err)
	}
	defer rows.Close()

	out := []Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, fmt.Errorf("list commands for runner %s: %w", runnerID, err)
		}
		out = append(out, c)
	}
	return out, wrap(rows.Err())
}

// ExpireStaleCommands fails commands that were queued long ago and never
// completed, so a dashboard does not show one pending for ever after an agent
// went away.
func (s *Store) ExpireStaleCommands(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("the age must be positive, got %s", olderThan)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE runner_commands
		SET status = 'failed', error = 'the agent did not carry this out in time', completed_at = now()
		WHERE status IN ('pending', 'delivered')
		  AND created_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("expire stale commands: %w", err)
	}
	return tag.RowsAffected(), nil
}
