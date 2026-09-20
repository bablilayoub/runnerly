package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bablilayoub/runnerly/internal/controlplane"
	"github.com/bablilayoub/runnerly/internal/supervisor"
)

// fakeControlPlane records what an agent reports to it.
type fakeControlPlane struct {
	mu         sync.Mutex
	events     []string
	heartbeats []string

	server *httptest.Server
}

func newFakeControlPlane(t *testing.T) *fakeControlPlane {
	t.Helper()
	f := &fakeControlPlane{}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/agent/events", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Events []controlplane.Event `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("events body: %v", err)
		}
		f.mu.Lock()
		for _, e := range body.Events {
			f.events = append(f.events, e.Event)
		}
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stored":` + itoa(len(body.Events)) + `}`))
	})
	mux.HandleFunc("POST /api/v1/agent/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var body controlplane.HeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("heartbeat body: %v", err)
		}
		f.mu.Lock()
		f.heartbeats = append(f.heartbeats, body.Status)
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runner":{"id":"r1","status":"online"},"config":{"heartbeat_interval_seconds":1}}`))
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func (f *fakeControlPlane) client() *controlplane.Client {
	return controlplane.New(f.server.URL, "rnr_machine_test",
		controlplane.WithHTTPClient(f.server.Client()))
}

func (f *fakeControlPlane) reported() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeControlPlane) statuses() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.heartbeats...)
}

// TestShutdownEventsReachTheControlPlane is a regression test.
//
// The reporter used to send with the agent's own context. On shutdown that
// context was already canceled, so the events explaining why the runner
// stopped were dropped before they left the process, and the failure was
// swallowed because the code treated a canceled context as "no need to
// log". A live run showed the control plane never receiving runner_stopping
// or runner_stopped. Reports now carry their own bounded context.
func TestShutdownEventsReachTheControlPlane(t *testing.T) {
	fake := newFakeControlPlane(t)
	r := installedRunner(t, "runnerly-01")

	proc := newFakeProcess()
	started := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgent(ctx, Options{
			Runner:            r,
			ControlPlane:      fake.client(),
			HeartbeatInterval: time.Hour, // only the final heartbeat should fire
			Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
				close(started)
				return proc, nil
			},
		})
	}()

	<-started
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return")
	}

	events := fake.reported()
	for _, want := range []string{"runner_starting", "runner_started", "runner_stopping", "runner_stopped"} {
		if !slicesContains(events, want) {
			t.Errorf("the control plane never received %q; got %v", want, events)
		}
	}

	// The last thing the server hears must be that the agent went offline on
	// purpose, rather than it having to time the runner out.
	statuses := fake.statuses()
	if len(statuses) == 0 {
		t.Fatal("no heartbeat was sent")
	}
	if last := statuses[len(statuses)-1]; last != statusOffline {
		t.Errorf("last reported status = %q, want %q", last, statusOffline)
	}
}

func TestSupervisorEventsBecomeControlPlaneEvents(t *testing.T) {
	fake := newFakeControlPlane(t)
	r := installedRunner(t, "runnerly-01")

	err := runAgent(context.Background(), Options{
		Runner:            r,
		ControlPlane:      fake.client(),
		HeartbeatInterval: time.Hour,
		Backoff:           supervisor.Backoff{Initial: time.Millisecond, Factor: 1, MaxRestarts: 1},
		Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
			p := newFakeProcess()
			p.finish(errProcessCrashed)
			return p, nil
		},
	})
	if err == nil {
		t.Fatal("Run() = nil, want the supervisor to give up")
	}

	events := fake.reported()
	for _, want := range []string{"runner_exited", "runner_restarting", "runner_gave_up"} {
		if !slicesContains(events, want) {
			t.Errorf("the control plane never received %q; got %v", want, events)
		}
	}
}

func TestReportingFailuresDoNotStopSupervision(t *testing.T) {
	// A control plane that refuses everything must not take the runner down
	// with it.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()

	r := installedRunner(t, "runnerly-01")
	client := controlplane.New(dead.URL, "rnr_machine_test", controlplane.WithHTTPClient(dead.Client()))

	proc := newFakeProcess()
	started := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgent(ctx, Options{
			Runner:            r,
			ControlPlane:      client,
			HeartbeatInterval: 5 * time.Millisecond,
			Start: func(supervisor.ProcessOptions) (supervisor.Process, error) {
				close(started)
				return proc, nil
			},
		})
	}()

	<-started
	// Let several reports fail.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v; a broken control plane must not fail the agent", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return")
	}
}

func TestEventsAreDroppedRatherThanBlockingSupervision(t *testing.T) {
	rep := newReporter(nil, testLogger(), time.Hour, "")

	// Nothing is draining the queue, so it fills and then drops.
	for range eventBuffer + 50 {
		rep.enqueue(controlplane.Event{Event: "noise"})
	}

	rep.mu.Lock()
	dropped := rep.dropped
	rep.mu.Unlock()

	if dropped != 50 {
		t.Errorf("dropped = %d, want 50", dropped)
	}
	if len(rep.events) != eventBuffer {
		t.Errorf("queue holds %d, want it capped at %d", len(rep.events), eventBuffer)
	}
}

func TestObserveMapsSupervisorEventsToStatuses(t *testing.T) {
	tests := []struct {
		kind supervisor.EventKind
		want string
	}{
		{supervisor.EventStarting, statusStarting},
		{supervisor.EventStarted, statusOnline},
		{supervisor.EventExited, statusStarting},
		{supervisor.EventGaveUp, statusError},
		{supervisor.EventStopping, statusStopping},
		{supervisor.EventStopped, statusOffline},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			rep := newReporter(nil, testLogger(), time.Hour, "")
			rep.observe(supervisor.Event{Kind: tt.kind})

			rep.mu.Lock()
			got := rep.status
			rep.mu.Unlock()

			if got != tt.want {
				t.Errorf("status after %s = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestSeverityForMarksTroubleAsTrouble(t *testing.T) {
	tests := map[supervisor.EventKind]string{
		supervisor.EventStarted:    "info",
		supervisor.EventExited:     "warn",
		supervisor.EventRestarting: "warn",
		supervisor.EventKilled:     "warn",
		supervisor.EventGaveUp:     "error",
	}
	for kind, want := range tests {
		if got, _ := severityFor(supervisor.Event{Kind: kind}); got != want {
			t.Errorf("severityFor(%s) = %q, want %q", kind, got, want)
		}
	}
}
