package agent

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/bablilayoub/runnerly/internal/controlplane"
	"github.com/bablilayoub/runnerly/internal/jobstate"
	"github.com/bablilayoub/runnerly/internal/supervisor"
	"github.com/bablilayoub/runnerly/internal/version"
)

// eventBuffer is how many events wait to be sent before the oldest are
// dropped.
//
// Reporting must never slow supervision down: if the control plane is slow or
// unreachable, keeping the runner alive matters more than keeping every
// event. Drops are counted and logged rather than hidden.
const eventBuffer = 256

// reporter forwards status and events to a control plane.
//
// Every method is safe to call from the supervisor's callback, and none of
// them block on the network.
type reporter struct {
	client   *controlplane.Client
	log      *slog.Logger
	interval time.Duration

	mu           sync.Mutex
	status       string
	statusDetail string
	runnerVer    string
	dropped      int

	events chan controlplane.Event
	// restart is called when the control plane asks for one. Nil means the
	// agent cannot honor the command and says so rather than dropping it.
	restart func() bool
	// jobState reports whether a job is running. Nil means the agent has no
	// job hooks installed and cannot tell, in which case it reports what it
	// does know rather than guessing at "busy".
	jobState func() jobstate.State
	// onToken persists a credential the control plane rotated.
	onToken func(string) error
	// lastToken is what was persisted, so the same rotation is not written
	// on every heartbeat.
	lastToken string
}

// Runner statuses the agent reports.
//
// "busy" comes from the job hooks, which the runner itself calls when a job
// starts and finishes. Without them installed the agent cannot tell a runner
// waiting for work from one flat out, and reports "online" rather than
// inventing an answer.
const (
	statusStarting = "starting"
	statusOnline   = "online"
	statusBusy     = "busy"
	statusStopping = "stopping"
	statusOffline  = "offline"
	statusError    = "error"
)

func newReporter(
	client *controlplane.Client, log *slog.Logger, interval time.Duration, runnerVer string,
) *reporter {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	return &reporter{
		client:    client,
		log:       log,
		interval:  interval,
		status:    statusStarting,
		runnerVer: runnerVer,
		events:    make(chan controlplane.Event, eventBuffer),
	}
}

// setStatus records what to report on the next heartbeat.
func (r *reporter) setStatus(status, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
	r.statusDetail = detail
}

// enqueue offers an event, dropping it if the buffer is full.
func (r *reporter) enqueue(e controlplane.Event) {
	select {
	case r.events <- e:
	default:
		r.mu.Lock()
		r.dropped++
		r.mu.Unlock()
	}
}

// observe turns a supervisor event into a status change and a reported event.
func (r *reporter) observe(e supervisor.Event) {
	switch e.Kind {
	case supervisor.EventStarting:
		r.setStatus(statusStarting, "")
	case supervisor.EventStarted:
		r.setStatus(statusOnline, "")
	case supervisor.EventExited:
		r.setStatus(statusStarting, "the runner exited and is being restarted")
	case supervisor.EventGaveUp:
		r.setStatus(statusError, "the runner failed too many times and is not being restarted")
	case supervisor.EventStopping:
		r.setStatus(statusStopping, "")
	case supervisor.EventStopped:
		r.setStatus(statusOffline, "")
	}

	severity, message := severityFor(e)
	r.enqueue(controlplane.Event{
		Event:    "runner_" + string(e.Kind),
		Severity: severity,
		Message:  message,
		Data:     eventData(e),
	})
}

func severityFor(e supervisor.Event) (severity, message string) {
	switch e.Kind {
	case supervisor.EventGaveUp:
		return "error", "the agent gave up restarting the runner"
	case supervisor.EventExited:
		return "warn", "the runner exited"
	case supervisor.EventRestarting:
		return "warn", "the runner is being restarted"
	case supervisor.EventKilled:
		return "warn", "the runner was killed after a graceful stop timed out"
	default:
		return "info", ""
	}
}

func eventData(e supervisor.Event) map[string]any {
	data := map[string]any{}
	if e.PID != 0 {
		data["pid"] = e.PID
	}
	if e.Attempt != 0 {
		data["attempt"] = e.Attempt
	}
	if e.Kind == supervisor.EventExited {
		data["exit_code"] = e.ExitCode
		data["uptime_seconds"] = e.Uptime.Seconds()
	}
	if e.Delay > 0 {
		data["delay_seconds"] = e.Delay.Seconds()
	}
	if e.Err != nil {
		data["error"] = e.Err.Error()
	}
	return data
}

// sendTimeout bounds one report. Reports deliberately do not use the
// agent's lifetime context: a request started just as the agent is shutting
// down would be canceled before it left, which is how the last events of a
// run went missing until this was separated out.
const sendTimeout = 10 * time.Second

// sendContext returns a context for a single request, independent of
// shutdown but bounded so a hung control plane cannot delay exit forever.
func sendContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), sendTimeout)
}

// run sends heartbeats and drains the event queue until the context ends.
//
// A failed report is logged and retried on the next tick rather than
// escalated: losing contact with the control plane must not stop a runner
// that is working.
func (r *reporter) run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// One last report so the dashboard learns the agent stopped on
			// purpose, instead of waiting for it to time out.
			r.finalReport()
			return

		case e := <-r.events:
			r.flushEvents(e)

		case <-ticker.C:
			r.heartbeat()
		}
	}
}

// flushEvents sends the event it was given plus anything else already queued.
func (r *reporter) flushEvents(first controlplane.Event) {
	batch := append([]controlplane.Event{first}, r.drain(maxBatch-1)...)

	ctx, cancel := sendContext()
	defer cancel()

	if _, err := r.client.SendEvents(ctx, batch); err != nil {
		r.log.Warn("could not report events to the control plane",
			"event", "report_failed", "count", len(batch), "error", err.Error())
	}
}

// maxBatch matches what the control plane accepts in one request.
const maxBatch = 100

// drain takes up to limit queued events without blocking.
func (r *reporter) drain(limit int) []controlplane.Event {
	var out []controlplane.Event
	for len(out) < limit {
		select {
		case e := <-r.events:
			out = append(out, e)
		default:
			return out
		}
	}
	return out
}

func (r *reporter) heartbeat() {
	r.mu.Lock()
	req := controlplane.HeartbeatRequest{
		Status:        r.status,
		StatusDetail:  r.statusDetail,
		RunnerVersion: r.runnerVer,
		AgentVersion:  version.Get().Short(),
	}
	dropped := r.dropped
	r.dropped = 0
	r.mu.Unlock()

	// A running job outranks "online": the process being up says nothing
	// about whether it is doing anything. Only override a healthy status,
	// so a runner that is stopping or has failed still reports that.
	if r.jobState != nil && req.Status == statusOnline {
		if job := r.jobState(); job.Running() {
			req.Status = statusBusy
			req.StatusDetail = job.Describe()
		}
	}

	if dropped > 0 {
		r.log.Warn("dropped events because the control plane was not keeping up",
			"event", "events_dropped", "count", dropped)
	}

	ctx, cancel := sendContext()
	defer cancel()

	resp, err := r.client.Heartbeat(ctx, req)
	if err == nil {
		r.persistToken(resp.MachineToken)
		r.runCommands(resp.Commands)
		return
	}
	if err != nil {
		if errors.Is(err, controlplane.ErrUnauthorized) {
			// Retrying will not help, so say so plainly and stop trying to
			// dress it up as a transient failure.
			r.log.Error("the control plane refused the machine token; reporting is stopping",
				"event", "report_unauthorized", "error", err.Error())
			return
		}
		r.log.Warn("could not send a heartbeat",
			"event", "report_failed", "error", err.Error())
	}
}

// persistToken writes down a credential the control plane rotated.
//
// The client is already using it by the time this runs. Failing to store it
// is worth a warning rather than an error: this run keeps working, and the
// next start re-enrolls.
func (r *reporter) persistToken(token string) {
	if token == "" || r.onToken == nil || token == r.lastToken {
		return
	}
	r.lastToken = token

	if err := r.onToken(token); err != nil {
		r.log.Warn("the control plane rotated this machine's credential but it could not be stored",
			"event", "token_store_failed",
			"error", err.Error(),
			"hint", "this run continues; the next start will have to enroll again")
		return
	}
	r.log.Info("stored a rotated machine credential", "event", "token_rotated")
}

// runCommands carries out what the control plane asked for and reports each
// outcome, so a command never sits "delivered" for ever.
func (r *reporter) runCommands(commands []controlplane.PendingCommand) {
	for _, c := range commands {
		failure := r.runCommand(c)

		ctx, cancel := sendContext()
		if err := r.client.CompleteCommand(ctx, c.ID, failure); err != nil {
			r.log.Warn("could not report a command result",
				"event", "command_result_failed", "command", c.Command, "error", err.Error())
		}
		cancel()
	}
}

// runCommand performs one command and returns why it failed, or "".
func (r *reporter) runCommand(c controlplane.PendingCommand) string {
	switch c.Command {
	case controlplane.CommandRestart:
		if r.restart == nil {
			return "this agent cannot restart its runner"
		}
		r.log.Info("restarting the runner because the control plane asked",
			"event", "restart_requested", "command", c.ID)

		if !r.restart() {
			// Nothing was running, which usually means it is already
			// between attempts and about to start anyway.
			return ""
		}
		return ""

	default:
		// A newer server may know commands this agent does not. Saying so
		// beats leaving the operator watching a command that never moves.
		r.log.Warn("the control plane asked for something this agent does not understand",
			"event", "unknown_command", "command", c.Command)
		return "this agent does not understand the command " + c.Command
	}
}

// finalReport tells the control plane the agent is going away, so the
// dashboard learns it stopped on purpose instead of waiting for it to time
// out.
//
// The events queued during shutdown are sent first: they are what explain
// why the runner stopped, and they are worthless once the heartbeat has
// already said "offline".
func (r *reporter) finalReport() {
	if remaining := r.drain(maxBatch); len(remaining) > 0 {
		ctx, cancel := sendContext()
		if _, err := r.client.SendEvents(ctx, remaining); err != nil {
			r.log.Warn("could not report the last events",
				"event", "report_failed", "count", len(remaining), "error", err.Error())
		}
		cancel()
	}

	r.setStatus(statusOffline, "the agent stopped")
	r.heartbeat()
}
