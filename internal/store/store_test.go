package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// DatabaseURLEnv names the environment variable that points the store tests
// at a PostgreSQL instance.
//
// These are integration tests against a real database, not a fake one: the
// store's job is to be correct about SQL, and a mock would only assert that
// Runnerly sends the strings Runnerly expects to send. Without the variable
// they skip, so `go test ./...` still works on a machine with no PostgreSQL.
const DatabaseURLEnv = "RUNNERLY_TEST_DATABASE_URL"

// testStore returns a store with its own freshly migrated schema.
//
// Every test gets a schema of its own because Go runs test packages in
// parallel against the one database: sharing "public" meant one package's
// setup dropped another package's tables mid-test.
//
// This duplicates internal/storetest, which the other packages use. An
// in-package test cannot import a helper that imports its own package, and
// the alternative — moving these tests out of the package — would give up
// access to the unexported pool they need.
func testStore(t *testing.T) *Store {
	t.Helper()

	url := os.Getenv(DatabaseURLEnv)
	if url == "" {
		t.Skipf("set %s to run the store tests", DatabaseURLEnv)
	}

	schema := testSchemaName(t)
	ctx := context.Background()

	s, err := Open(ctx, Options{URL: url, Schema: schema})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := s.CreateSchema(ctx, schema); err != nil {
		s.Close()
		t.Fatalf("create the test schema: %v", err)
	}
	t.Cleanup(func() {
		if err := s.DropSchema(context.Background(), schema); err != nil {
			t.Errorf("could not drop the test schema %s: %v", schema, err)
		}
		s.Close()
	})

	if _, err := s.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return s
}

func testSchemaName(t *testing.T) string {
	t.Helper()

	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, t.Name())
	if len(cleaned) > 40 {
		cleaned = cleaned[:40]
	}
	return "t_" + cleaned + "_" + hex.EncodeToString(b[:])
}

func sampleRunner(name string) RegisterRunner {
	return RegisterRunner{
		Name:          name,
		GitHubHost:    "github.com",
		GitHubScope:   ScopeRepository,
		GitHubScopeID: "acme/widgets",
		OS:            "linux",
		Architecture:  "x64",
		CPUCount:      8,
		MemoryBytes:   16 << 30,
		DiskBytes:     420 << 30,
		Labels:        []string{"self-hosted", "linux", "x64"},
		AgentVersion:  "0.1.0",
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// testStore already migrated this schema; a second run must do nothing.
	ran, err := s.Migrate(ctx)
	if err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if len(ran) != 0 {
		t.Errorf("second Migrate() applied %v, want nothing", ran)
	}
}

func TestMigrationsAreOrdered(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations() error = %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations were embedded")
	}
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].Name >= migrations[i].Name {
			t.Errorf("migrations are not sorted: %q before %q", migrations[i-1].Name, migrations[i].Name)
		}
	}
}

func TestUpsertRunnerIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatalf("UpsertRunner() error = %v", err)
	}
	if first.ID == "" || first.Status != StatusStarting {
		t.Errorf("runner = %+v", first)
	}
	if len(first.Labels) != 3 {
		t.Errorf("Labels = %v", first.Labels)
	}

	// The same machine enrolling again must not create a second row.
	updated := sampleRunner("runnerly-01")
	updated.CPUCount = 16
	updated.Labels = []string{"self-hosted", "linux", "x64", "docker"}

	second, err := s.UpsertRunner(ctx, updated)
	if err != nil {
		t.Fatalf("second UpsertRunner() error = %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("a re-enrolling machine got a new id: %s then %s", first.ID, second.ID)
	}
	if second.CPUCount != 16 || len(second.Labels) != 4 {
		t.Errorf("the second enrollment did not update the row: %+v", second)
	}

	all, err := s.ListRunners(ctx, ListRunnersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("got %d runners, want 1", len(all))
	}
}

func TestRunnersAreScopedByHostScopeAndName(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// The same name in a different repository is a different runner.
	other := sampleRunner("runnerly-01")
	other.GitHubScopeID = "acme/gadgets"

	if _, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertRunner(ctx, other); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListRunners(ctx, ListRunnersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d runners, want 2", len(all))
	}

	scoped, err := s.ListRunners(ctx, ListRunnersOptions{Scope: "acme/gadgets"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 || scoped[0].GitHubScopeID != "acme/gadgets" {
		t.Errorf("scoped listing = %+v", scoped)
	}
}

func TestUpsertRunnerNeedsAName(t *testing.T) {
	s := testStore(t)
	in := sampleRunner("")
	if _, err := s.UpsertRunner(context.Background(), in); err == nil {
		t.Error("UpsertRunner() accepted a runner with no name")
	}
}

func TestHeartbeatUpdatesStatusAndFreshness(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	if r.LastHeartbeat != nil {
		t.Error("a freshly registered runner already had a heartbeat")
	}

	updated, err := s.RecordHeartbeat(ctx, r.ID, Heartbeat{
		Status:        StatusBusy,
		CPUPercent:    31.2,
		MemoryPercent: 42,
		DiskPercent:   67,
		RunnerVersion: "2.337.0",
		AgentVersion:  "0.2.0",
	})
	if err != nil {
		t.Fatalf("RecordHeartbeat() error = %v", err)
	}
	if updated.Status != StatusBusy {
		t.Errorf("Status = %q, want busy", updated.Status)
	}
	if updated.LastHeartbeat == nil {
		t.Fatal("LastHeartbeat was not stamped")
	}
	if updated.CPUPercent != 31.2 || updated.RunnerVersion != "2.337.0" {
		t.Errorf("metrics were not stored: %+v", updated)
	}

	now := time.Now()
	if got := updated.Health(now, DefaultThresholds()); got != HealthOK {
		t.Errorf("Health() = %q, want healthy right after a heartbeat", got)
	}
}

func TestHeartbeatKeepsAVersionItWasNotTold(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordHeartbeat(ctx, r.ID, Heartbeat{RunnerVersion: "2.337.0"}); err != nil {
		t.Fatal(err)
	}
	// A later heartbeat that omits the version must not blank it.
	after, err := s.RecordHeartbeat(ctx, r.ID, Heartbeat{})
	if err != nil {
		t.Fatal(err)
	}
	if after.RunnerVersion != "2.337.0" {
		t.Errorf("RunnerVersion = %q, want it preserved", after.RunnerVersion)
	}
}

func TestHeartbeatForAnUnknownRunner(t *testing.T) {
	s := testStore(t)
	_, err := s.RecordHeartbeat(context.Background(), newID(), Heartbeat{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestHealthThresholds(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()

	at := func(d time.Duration) Runner {
		seen := now.Add(-d)
		return Runner{Status: StatusOnline, LastHeartbeat: &seen}
	}

	tests := []struct {
		age  time.Duration
		want Health
	}{
		{time.Second, HealthOK},
		{29 * time.Second, HealthOK},
		{30 * time.Second, HealthStale},
		{89 * time.Second, HealthStale},
		{90 * time.Second, HealthOffline},
		{time.Hour, HealthOffline},
	}
	for _, tt := range tests {
		if got := at(tt.age).Health(now, th); got != tt.want {
			t.Errorf("Health() at %v = %q, want %q", tt.age, got, tt.want)
		}
	}

	// A runner that never reported is offline, not healthy.
	if got := (Runner{Status: StatusOnline}).Health(now, th); got != HealthOffline {
		t.Errorf("Health() with no heartbeat = %q, want offline", got)
	}
}

func TestEffectiveStatusOverridesAStaleClaim(t *testing.T) {
	now := time.Now()
	th := DefaultThresholds()

	seen := now.Add(-2 * time.Hour)
	stopped := Runner{Status: StatusOnline, LastHeartbeat: &seen}
	if got := stopped.EffectiveStatus(now, th); got != StatusOffline {
		t.Errorf("EffectiveStatus() = %q, want offline for a runner that stopped reporting", got)
	}

	fresh := now.Add(-time.Second)
	busy := Runner{Status: StatusBusy, LastHeartbeat: &fresh}
	if got := busy.EffectiveStatus(now, th); got != StatusBusy {
		t.Errorf("EffectiveStatus() = %q, want busy", got)
	}
}

func TestCountRunners(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	online, err := s.UpsertRunner(ctx, sampleRunner("online-1"))
	if err != nil {
		t.Fatal(err)
	}
	busy, err := s.UpsertRunner(ctx, sampleRunner("busy-1"))
	if err != nil {
		t.Fatal(err)
	}
	// Registered but never heard from.
	if _, err := s.UpsertRunner(ctx, sampleRunner("silent-1")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.RecordHeartbeat(ctx, online.ID, Heartbeat{Status: StatusOnline}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordHeartbeat(ctx, busy.ID, Heartbeat{Status: StatusBusy}); err != nil {
		t.Fatal(err)
	}

	counts, err := s.CountRunners(ctx, time.Now(), DefaultThresholds())
	if err != nil {
		t.Fatalf("CountRunners() error = %v", err)
	}
	if counts.Total != 3 {
		t.Errorf("Total = %d, want 3", counts.Total)
	}
	if counts.Online != 1 {
		t.Errorf("Online = %d, want 1", counts.Online)
	}
	if counts.Busy != 1 {
		t.Errorf("Busy = %d, want 1", counts.Busy)
	}
	if counts.Offline != 1 {
		t.Errorf("Offline = %d, want 1 (the runner that never reported)", counts.Offline)
	}
}

func TestDeleteRunner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRunner(ctx, r.ID); err != nil {
		t.Fatalf("DeleteRunner() error = %v", err)
	}
	if _, err := s.Runner(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteRunner(ctx, r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice = %v, want ErrNotFound", err)
	}
}

func TestDeletingARunnerTakesItsTokensAndEvents(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	machineToken, err := s.IssueMachineToken(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordEvent(ctx, NewEvent{RunnerID: r.ID, Event: "runner_online"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteRunner(ctx, r.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.AuthenticateMachine(ctx, machineToken); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("a deleted runner's token still authenticates: %v", err)
	}
	events, err := s.ListEvents(ctx, ListEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events after deleting the runner, want 0", len(events))
	}
}

func TestSetGitHubRunnerID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	if r.GitHubRunnerID != nil {
		t.Error("a new runner already had a GitHub id")
	}
	if err := s.SetGitHubRunnerID(ctx, r.ID, 42); err != nil {
		t.Fatalf("SetGitHubRunnerID() error = %v", err)
	}

	after, err := s.Runner(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.GitHubRunnerID == nil || *after.GitHubRunnerID != 42 {
		t.Errorf("GitHubRunnerID = %v, want 42", after.GitHubRunnerID)
	}

	if err := s.SetGitHubRunnerID(ctx, newID(), 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListRunnersRespectsTheLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := range 5 {
		if _, err := s.UpsertRunner(ctx, sampleRunner(fmt.Sprintf("runner-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListRunners(ctx, ListRunnersOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d runners, want 2", len(got))
	}
}

func TestListRunnersReturnsAnEmptySliceNotNil(t *testing.T) {
	s := testStore(t)
	got, err := s.ListRunners(context.Background(), ListRunnersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Error("ListRunners() = nil; an empty listing should encode as [] in JSON, not null")
	}
}

func TestNewIDLooksLikeAUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := newID()
		if len(id) != 36 {
			t.Fatalf("id %q is %d characters, want 36", id, len(id))
		}
		if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			t.Fatalf("id %q is not dash-separated in the right places", id)
		}
		if id[14] != '4' {
			t.Fatalf("id %q is not version 4", id)
		}
		if !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatalf("id %q has the wrong variant nibble", id)
		}
		if seen[id] {
			t.Fatalf("id %q was generated twice", id)
		}
		seen[id] = true
	}
}

func TestOpenRejectsAnEmptyURL(t *testing.T) {
	_, err := Open(context.Background(), Options{})
	if err == nil {
		t.Fatal("Open() accepted an empty URL")
	}
	if !strings.Contains(err.Error(), "RUNNERLY_DATABASE_URL") {
		t.Errorf("the error should say how to set it, got: %v", err)
	}
}

func TestSchemaNameIsValidated(t *testing.T) {
	// The name is interpolated into SQL that cannot be parameterized, so
	// anything but a plain identifier is refused rather than escaped.
	for _, bad := range []string{
		"public; DROP TABLE runners",
		"Public",
		"1schema",
		"",
		"with space",
		`quoted"`,
	} {
		if err := validateSchemaName(bad); err == nil {
			t.Errorf("validateSchemaName(%q) = nil, want an error", bad)
		}
	}
	for _, good := range []string{"public", "runnerly", "t_abc_123", "_private"} {
		if err := validateSchemaName(good); err != nil {
			t.Errorf("validateSchemaName(%q) = %v", good, err)
		}
	}
}

func TestOpenRejectsABadSchemaName(t *testing.T) {
	url := os.Getenv(DatabaseURLEnv)
	if url == "" {
		t.Skipf("set %s to run the store tests", DatabaseURLEnv)
	}
	if _, err := Open(context.Background(), Options{URL: url, Schema: "not valid"}); err == nil {
		t.Error("Open() accepted an invalid schema name")
	}
}
