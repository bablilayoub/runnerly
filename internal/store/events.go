package store

import (
	"context"
	"fmt"
)

// NewEvent is something to record about a runner.
type NewEvent struct {
	// RunnerID may be empty for an event that belongs to no single runner.
	RunnerID string
	Event    string
	Severity string
	Message  string
	Data     map[string]any
}

// RecordEvent stores one event.
func (s *Store) RecordEvent(ctx context.Context, in NewEvent) (Event, error) {
	if in.Event == "" {
		return Event{}, fmt.Errorf("an event needs a name")
	}
	if in.Severity == "" {
		in.Severity = SeverityInfo
	}
	if in.Data == nil {
		in.Data = map[string]any{}
	}

	var runnerID *string
	if in.RunnerID != "" {
		runnerID = &in.RunnerID
	}

	var e Event
	row := s.pool.QueryRow(ctx, `
		INSERT INTO runner_events (runner_id, event, severity, message, data)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, runner_id, event, severity, message, data, created_at`,
		runnerID, in.Event, in.Severity, in.Message, in.Data)

	if err := row.Scan(&e.ID, &e.RunnerID, &e.Event, &e.Severity, &e.Message, &e.Data, &e.CreatedAt); err != nil {
		return Event{}, fmt.Errorf("record event %q: %w", in.Event, wrap(err))
	}
	return e, nil
}

// RecordEvents stores several events in one round trip. Agents batch what
// happened between heartbeats, so this is the common path.
func (s *Store) RecordEvents(ctx context.Context, events []NewEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("record events: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	stored := 0
	for _, in := range events {
		if in.Event == "" {
			continue
		}
		if in.Severity == "" {
			in.Severity = SeverityInfo
		}
		if in.Data == nil {
			in.Data = map[string]any{}
		}
		var runnerID *string
		if in.RunnerID != "" {
			runnerID = &in.RunnerID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO runner_events (runner_id, event, severity, message, data)
			VALUES ($1, $2, $3, $4, $5)`,
			runnerID, in.Event, in.Severity, in.Message, in.Data); err != nil {
			return stored, fmt.Errorf("record event %q: %w", in.Event, err)
		}
		stored++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("record events: %w", err)
	}
	return stored, nil
}

// ListEventsOptions filters the event feed.
type ListEventsOptions struct {
	// RunnerID narrows to one runner. Empty returns events for all of them.
	RunnerID string
	// Severity narrows to one severity. Empty returns all.
	Severity string
	// Before returns events older than this id, for paging back through the
	// feed. Zero starts at the newest.
	Before int64
	Limit  int
}

// ListEvents returns events, newest first.
func (s *Store) ListEvents(ctx context.Context, opts ListEventsOptions) ([]Event, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, runner_id, event, severity, message, data, created_at
		FROM runner_events
		WHERE ($1 = '' OR runner_id::text = $1)
		  AND ($2 = '' OR severity = $2)
		  AND ($3 = 0 OR id < $3)
		ORDER BY id DESC
		LIMIT $4`, opts.RunnerID, opts.Severity, opts.Before, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.RunnerID, &e.Event, &e.Severity, &e.Message, &e.Data, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("list events: %w", err)
		}
		out = append(out, e)
	}
	return out, wrap(rows.Err())
}

// PruneEvents deletes events older than the given number of days and returns
// how many went.
//
// The control plane is not a log platform: the event feed is for recent
// history, and something has to stop the table growing without limit.
func (s *Store) PruneEvents(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		return 0, fmt.Errorf("retention must be at least one day, got %d", days)
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM runner_events WHERE created_at < now() - make_interval(days => $1)`, days)
	if err != nil {
		return 0, fmt.Errorf("prune events: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RecordAudit stores an operator action.
func (s *Store) RecordAudit(ctx context.Context, actor, action, target string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO audit_logs (actor, action, target, detail) VALUES ($1, $2, $3, $4)`,
		actor, action, target, detail); err != nil {
		return fmt.Errorf("record audit entry: %w", err)
	}
	return nil
}
