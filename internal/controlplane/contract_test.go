package controlplane_test

// This is a contract test. The agent's client declares its own wire types so
// the agent binary does not have to carry a PostgreSQL driver, which means
// nothing but this file stops the two sides drifting apart. It runs a real
// server against a real database and drives it with the real client.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bablilayoub/runnerly/internal/controlplane"
	"github.com/bablilayoub/runnerly/internal/server"
	"github.com/bablilayoub/runnerly/internal/store"
	"github.com/bablilayoub/runnerly/internal/storetest"
)

// liveServer returns a running control plane with a database schema of its
// own, so this suite cannot collide with the server or store tests running
// alongside it.
func liveServer(t *testing.T) (string, *store.Store) {
	t.Helper()

	db := storetest.New(t)

	srv, err := server.New(server.Options{Store: db, HeartbeatInterval: 25 * time.Second})
	if err != nil {
		t.Fatalf("server.New() error = %v", err)
	}
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	return httpSrv.URL, db
}

func enrollmentToken(t *testing.T, db *store.Store) string {
	t.Helper()
	_, secret, err := db.CreateEnrollmentToken(context.Background(), "contract test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func sampleRegistration() controlplane.RegisterRequest {
	return controlplane.RegisterRequest{
		Name:          "runnerly-01",
		GitHubHost:    "github.com",
		GitHubScope:   store.ScopeRepository,
		GitHubScopeID: "acme/widgets",
		OS:            "linux",
		Architecture:  "x64",
		CPUCount:      8,
		MemoryBytes:   16 << 30,
		DiskBytes:     420 << 30,
		Labels:        []string{"self-hosted", "linux", "x64"},
		RunnerVersion: "2.337.0",
		AgentVersion:  "0.3.0",
	}
}

func TestEnrollHeartbeatAndEventsAgainstARealServer(t *testing.T) {
	baseURL, db := liveServer(t)
	ctx := context.Background()

	// Enrollment, with no credential but the one-shot token.
	registered, err := controlplane.New(baseURL, "").
		Register(ctx, enrollmentToken(t, db), sampleRegistration())
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.Runner.ID == "" || registered.Runner.Name != "runnerly-01" {
		t.Errorf("runner = %+v", registered.Runner)
	}
	if !strings.HasPrefix(registered.MachineToken, store.MachinePrefix) {
		t.Errorf("machine token = %q", registered.MachineToken)
	}
	// The server's interval must survive the wire, not fall back to a default.
	if got := registered.Config.Interval(); got != 25*time.Second {
		t.Errorf("Interval() = %v, want 25s from the server", got)
	}

	// Everything after enrollment uses the machine token.
	client := controlplane.New(baseURL, registered.MachineToken)

	beat, err := client.Heartbeat(ctx, controlplane.HeartbeatRequest{
		Status:        store.StatusBusy,
		CPUPercent:    31.2,
		MemoryPercent: 42,
		DiskPercent:   67,
		AgentVersion:  "0.3.0",
	})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if beat.Runner.Status != store.StatusBusy {
		t.Errorf("Status = %q, want busy", beat.Runner.Status)
	}

	// The fields really landed in the database, not just in the response.
	stored, err := db.Runner(ctx, registered.Runner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CPUPercent != 31.2 || stored.DiskPercent != 67 {
		t.Errorf("metrics did not survive the round trip: %+v", stored)
	}
	if stored.LastHeartbeat == nil {
		t.Error("the heartbeat was not stamped")
	}
	if stored.CPUCount != 8 || stored.MemoryBytes != 16<<30 {
		t.Errorf("hardware from enrollment did not survive: %+v", stored)
	}
	if strings.Join(stored.Labels, ",") != "self-hosted,linux,x64" {
		t.Errorf("labels = %v", stored.Labels)
	}

	n, err := client.SendEvents(ctx, []controlplane.Event{
		{Event: "runner_online", Severity: store.SeverityInfo, Message: "up", Data: map[string]any{"pid": 42}},
		{Event: "runner_exited", Severity: store.SeverityWarn},
	})
	if err != nil {
		t.Fatalf("SendEvents() error = %v", err)
	}
	if n != 2 {
		t.Errorf("stored %d events, want 2", n)
	}

	events, err := db.ListEvents(ctx, store.ListEventsOptions{RunnerID: registered.Runner.ID})
	if err != nil {
		t.Fatal(err)
	}
	var sawOnline bool
	for _, e := range events {
		if e.Event == "runner_online" {
			sawOnline = true
			if e.Data["pid"] != float64(42) {
				t.Errorf("event data did not survive: %v", e.Data)
			}
		}
	}
	if !sawOnline {
		t.Errorf("the events are not in the feed: %+v", events)
	}
}

func TestSendEventsWithNothingToSendMakesNoRequest(t *testing.T) {
	baseURL, db := liveServer(t)
	registered, err := controlplane.New(baseURL, "").
		Register(context.Background(), enrollmentToken(t, db), sampleRegistration())
	if err != nil {
		t.Fatal(err)
	}

	client := controlplane.New(baseURL, registered.MachineToken)
	if n, err := client.SendEvents(context.Background(), nil); err != nil || n != 0 {
		t.Errorf("SendEvents(nil) = %d, %v", n, err)
	}
}

func TestARefusedCredentialIsReportedAsUnauthorized(t *testing.T) {
	baseURL, _ := liveServer(t)
	client := controlplane.New(baseURL, "rnr_machine_not_a_real_token")

	_, err := client.Heartbeat(context.Background(), controlplane.HeartbeatRequest{})
	if !errors.Is(err, controlplane.ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	// The agent should be able to show the server's hint to an operator.
	if !strings.Contains(err.Error(), "Enroll the machine again") {
		t.Errorf("the error lost the server's hint: %v", err)
	}
}

func TestAnExhaustedEnrollmentTokenIsReported(t *testing.T) {
	baseURL, db := liveServer(t)
	ctx := context.Background()

	once := 1
	_, secret, err := db.CreateEnrollmentToken(ctx, "single use", &once, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controlplane.New(baseURL, "").Register(ctx, secret, sampleRegistration()); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}

	second := sampleRegistration()
	second.Name = "runnerly-02"
	_, err = controlplane.New(baseURL, "").Register(ctx, secret, second)
	if !errors.Is(err, controlplane.ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized for a used-up token", err)
	}
}

func TestHealthNeedsNoCredential(t *testing.T) {
	baseURL, _ := liveServer(t)
	if err := controlplane.New(baseURL, "").Health(context.Background()); err != nil {
		t.Errorf("Health() error = %v", err)
	}
}

func TestAnUnreachableControlPlaneIsReported(t *testing.T) {
	client := controlplane.New("http://127.0.0.1:1", "rnr_machine_x",
		controlplane.WithHTTPClient(&http.Client{Timeout: time.Second}))

	if err := client.Health(context.Background()); err == nil {
		t.Error("Health() succeeded against a closed port")
	}
	if _, err := client.Heartbeat(context.Background(), controlplane.HeartbeatRequest{}); err == nil {
		t.Error("Heartbeat() succeeded against a closed port")
	} else if errors.Is(err, controlplane.ErrUnauthorized) {
		t.Errorf("a network failure was reported as an auth failure: %v", err)
	}
}

// TestRestartReachesTheAgentOnItsHeartbeat exercises the whole command path
// against a real server: queue, deliver, report.
func TestRestartReachesTheAgentOnItsHeartbeat(t *testing.T) {
	baseURL, db := liveServer(t)
	ctx := context.Background()

	registered, err := controlplane.New(baseURL, "").
		Register(ctx, enrollmentToken(t, db), sampleRegistration())
	if err != nil {
		t.Fatal(err)
	}
	client := controlplane.New(baseURL, registered.MachineToken)

	queued, err := db.QueueCommand(ctx, registered.Runner.ID, store.CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}

	beat, err := client.Heartbeat(ctx, controlplane.HeartbeatRequest{Status: store.StatusOnline})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if len(beat.Commands) != 1 {
		t.Fatalf("commands = %+v, want the queued restart", beat.Commands)
	}
	if beat.Commands[0].ID != queued.ID || beat.Commands[0].Command != controlplane.CommandRestart {
		t.Errorf("command = %+v", beat.Commands[0])
	}

	if err := client.CompleteCommand(ctx, beat.Commands[0].ID, ""); err != nil {
		t.Fatalf("CompleteCommand() error = %v", err)
	}

	commands, err := db.ListCommands(ctx, registered.Runner.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if commands[0].Status != store.CommandDone {
		t.Errorf("status = %q, want done", commands[0].Status)
	}
}

func TestAFailedCommandIsReportedWithItsReason(t *testing.T) {
	baseURL, db := liveServer(t)
	ctx := context.Background()

	registered, err := controlplane.New(baseURL, "").
		Register(ctx, enrollmentToken(t, db), sampleRegistration())
	if err != nil {
		t.Fatal(err)
	}
	client := controlplane.New(baseURL, registered.MachineToken)

	queued, err := db.QueueCommand(ctx, registered.Runner.ID, store.CommandRestart, "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, controlplane.HeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := client.CompleteCommand(ctx, queued.ID, "the runner would not stop"); err != nil {
		t.Fatal(err)
	}

	commands, err := db.ListCommands(ctx, registered.Runner.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if commands[0].Status != store.CommandFailed || commands[0].Error != "the runner would not stop" {
		t.Errorf("command = %+v", commands[0])
	}
}
