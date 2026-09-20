package store

import (
	"context"
	"testing"
)

func TestRecordAndListEvents(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}

	e, err := s.RecordEvent(ctx, NewEvent{
		RunnerID: r.ID,
		Event:    "runner_online",
		Severity: SeverityInfo,
		Message:  "runner is running",
		Data:     map[string]any{"pid": 9804},
	})
	if err != nil {
		t.Fatalf("RecordEvent() error = %v", err)
	}
	if e.ID == 0 || e.RunnerID == nil || *e.RunnerID != r.ID {
		t.Errorf("event = %+v", e)
	}
	if e.Data["pid"] != float64(9804) && e.Data["pid"] != 9804 {
		t.Errorf("Data = %v, want the pid preserved", e.Data)
	}

	events, err := s.ListEvents(ctx, ListEventsOptions{RunnerID: r.ID})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestEventNeedsAName(t *testing.T) {
	s := testStore(t)
	if _, err := s.RecordEvent(context.Background(), NewEvent{Event: ""}); err == nil {
		t.Error("RecordEvent() accepted an event with no name")
	}
}

func TestEventsWithoutARunner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.RecordEvent(ctx, NewEvent{Event: "server_started"}); err != nil {
		t.Fatalf("RecordEvent() error = %v", err)
	}
	events, err := s.ListEvents(ctx, ListEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].RunnerID != nil {
		t.Errorf("events = %+v", events)
	}
}

func TestRecordEventsBatches(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}

	batch := []NewEvent{
		{RunnerID: r.ID, Event: "runner_starting"},
		{RunnerID: r.ID, Event: "runner_online", Severity: SeverityInfo},
		{RunnerID: r.ID, Event: "runner_exited", Severity: SeverityWarn},
		{Event: ""}, // skipped rather than failing the batch
	}
	stored, err := s.RecordEvents(ctx, batch)
	if err != nil {
		t.Fatalf("RecordEvents() error = %v", err)
	}
	if stored != 3 {
		t.Errorf("stored %d events, want 3", stored)
	}

	if n, err := s.RecordEvents(ctx, nil); err != nil || n != 0 {
		t.Errorf("RecordEvents(nil) = %d, %v", n, err)
	}
}

func TestListEventsFiltersAndPages(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.UpsertRunner(ctx, sampleRunner("runnerly-02"))
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range []NewEvent{
		{RunnerID: r.ID, Event: "a", Severity: SeverityInfo},
		{RunnerID: r.ID, Event: "b", Severity: SeverityWarn},
		{RunnerID: r.ID, Event: "c", Severity: SeverityError},
		{RunnerID: other.ID, Event: "d", Severity: SeverityInfo},
	} {
		if _, err := s.RecordEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	mine, err := s.ListEvents(ctx, ListEventsOptions{RunnerID: r.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 3 {
		t.Errorf("got %d events for one runner, want 3", len(mine))
	}
	// Newest first.
	if mine[0].Event != "c" {
		t.Errorf("first event = %q, want the newest", mine[0].Event)
	}

	warnings, err := s.ListEvents(ctx, ListEventsOptions{Severity: SeverityWarn})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].Event != "b" {
		t.Errorf("severity filter returned %+v", warnings)
	}

	page, err := s.ListEvents(ctx, ListEventsOptions{RunnerID: r.ID, Before: mine[0].ID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Event != "b" {
		t.Errorf("paging returned %+v, want the event before the newest", page)
	}
}

func TestPruneEvents(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.RecordEvent(ctx, NewEvent{Event: "recent"}); err != nil {
		t.Fatal(err)
	}
	// Age one row past the retention window.
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO runner_events (event, created_at) VALUES ('old', now() - interval '40 days')`); err != nil {
		t.Fatal(err)
	}

	removed, err := s.PruneEvents(ctx, 30)
	if err != nil {
		t.Fatalf("PruneEvents() error = %v", err)
	}
	if removed != 1 {
		t.Errorf("pruned %d events, want 1", removed)
	}

	left, err := s.ListEvents(ctx, ListEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Event != "recent" {
		t.Errorf("events after pruning = %+v", left)
	}

	if _, err := s.PruneEvents(ctx, 0); err == nil {
		t.Error("PruneEvents(0) was accepted; it would delete everything")
	}
}

func TestRecordAudit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.RecordAudit(ctx, "octocat", "runner.delete", "runnerly-01", map[string]any{"reason": "retired"}); err != nil {
		t.Fatalf("RecordAudit() error = %v", err)
	}

	var actor, action, target string
	if err := s.pool.QueryRow(ctx,
		`SELECT actor, action, target FROM audit_logs ORDER BY id DESC LIMIT 1`).
		Scan(&actor, &action, &target); err != nil {
		t.Fatal(err)
	}
	if actor != "octocat" || action != "runner.delete" || target != "runnerly-01" {
		t.Errorf("audit row = %s %s %s", actor, action, target)
	}
}
