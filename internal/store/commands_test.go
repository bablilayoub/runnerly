package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueueAndTakeCommand(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}

	queued, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatalf("QueueCommand() error = %v", err)
	}
	if queued.Status != CommandPending || queued.RequestedBy != "octocat" {
		t.Errorf("command = %+v", queued)
	}

	taken, err := s.TakeCommands(ctx, r.ID)
	if err != nil {
		t.Fatalf("TakeCommands() error = %v", err)
	}
	if len(taken) != 1 || taken[0].ID != queued.ID {
		t.Fatalf("took %+v", taken)
	}
	if taken[0].Status != CommandDelivered || taken[0].DeliveredAt == nil {
		t.Errorf("delivery was not recorded: %+v", taken[0])
	}

	// A second heartbeat must not receive it again.
	again, err := s.TakeCommands(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("the same command was delivered twice: %+v", again)
	}
}

func TestQueueCommandDoesNotDuplicate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	// Pressing the button twice must not restart the runner twice.
	second, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("a second request queued another command: %s then %s", first.ID, second.ID)
	}

	// Once it is done, a new one may be queued.
	if err := s.CompleteCommand(ctx, r.ID, first.ID, ""); err != nil {
		t.Fatal(err)
	}
	third, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID {
		t.Error("a completed command was reused instead of queuing a new one")
	}
}

func TestQueueCommandRejectsWhatAgentsDoNotUnderstand(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueCommand(ctx, r.ID, "self-destruct", "octocat"); err == nil {
		t.Error("QueueCommand() accepted a command no agent understands")
	}
}

func TestCompleteCommandRecordsFailure(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteCommand(ctx, r.ID, queued.ID, "the runner would not stop"); err != nil {
		t.Fatalf("CompleteCommand() error = %v", err)
	}

	commands, err := s.ListCommands(ctx, r.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("got %d commands, want 1", len(commands))
	}
	if commands[0].Status != CommandFailed || commands[0].Error == "" {
		t.Errorf("command = %+v", commands[0])
	}
	if commands[0].CompletedAt == nil {
		t.Error("CompletedAt was not stamped")
	}
}

func TestOneAgentCannotCompleteAnothersCommand(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	mine, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.UpsertRunner(ctx, sampleRunner("runnerly-02"))
	if err != nil {
		t.Fatal(err)
	}

	queued, err := s.QueueCommand(ctx, mine.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteCommand(ctx, other.ID, queued.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound: a runner closed another's command", err)
	}
}

func TestCompleteCommandTwiceIsRefused(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteCommand(ctx, r.ID, queued.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteCommand(ctx, r.ID, queued.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound for an already-completed command", err)
	}
}

func TestExpireStaleCommands(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE runner_commands SET created_at = now() - interval '2 hours' WHERE id = $1`,
		queued.ID); err != nil {
		t.Fatal(err)
	}

	expired, err := s.ExpireStaleCommands(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ExpireStaleCommands() error = %v", err)
	}
	if expired != 1 {
		t.Errorf("expired %d commands, want 1", expired)
	}

	commands, err := s.ListCommands(ctx, r.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if commands[0].Status != CommandFailed {
		t.Errorf("status = %q, want failed", commands[0].Status)
	}
	// Once expired it must not still be handed to an agent.
	taken, err := s.TakeCommands(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(taken) != 0 {
		t.Errorf("an expired command was still delivered: %+v", taken)
	}

	if _, err := s.ExpireStaleCommands(ctx, 0); err == nil {
		t.Error("ExpireStaleCommands(0) was accepted; it would fail every live command")
	}
}

func TestDeletingARunnerTakesItsCommands(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueCommand(ctx, r.ID, CommandRestart, "octocat"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRunner(ctx, r.ID); err != nil {
		t.Fatal(err)
	}

	commands, err := s.ListCommands(ctx, r.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Errorf("got %d commands after deleting the runner, want 0", len(commands))
	}
}
