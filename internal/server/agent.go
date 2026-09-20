package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/bablilayoub/runnerly/internal/store"
)

// RegisterRequest is what an agent sends to enroll.
type RegisterRequest struct {
	Name          string   `json:"name"`
	GitHubHost    string   `json:"github_host"`
	GitHubScope   string   `json:"github_scope"`
	GitHubScopeID string   `json:"github_scope_id"`
	OS            string   `json:"os"`
	Architecture  string   `json:"architecture"`
	CPUCount      int      `json:"cpu_count"`
	MemoryBytes   int64    `json:"memory_bytes"`
	DiskBytes     int64    `json:"disk_bytes"`
	Labels        []string `json:"labels"`
	Ephemeral     bool     `json:"ephemeral"`
	RunnerVersion string   `json:"runner_version"`
	AgentVersion  string   `json:"agent_version"`
}

// RegisterResponse carries the credential the agent uses from then on.
type RegisterResponse struct {
	Runner store.Runner `json:"runner"`
	// MachineToken is returned once. The server keeps only its hash.
	MachineToken string      `json:"machine_token"`
	Config       AgentConfig `json:"config"`
}

// AgentConfig is what the server tells an agent about how to behave.
type AgentConfig struct {
	HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds"`
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		s.fail(w, r, http.StatusUnauthorized, "no_credentials",
			"Enrolling needs an enrollment token.",
			"Send it as `Authorization: Bearer rnr_enroll_...`. Create one with "+
				"`runnerly server enrollment-token create`.")
		return
	}

	var req RegisterRequest
	if !s.decode(w, r, &req) {
		return
	}
	if req.Name == "" || req.GitHubScopeID == "" {
		s.fail(w, r, http.StatusBadRequest, "invalid_request",
			"A runner needs a name and a GitHub scope.",
			"Send name and github_scope_id, for example \"runnerly-01\" and \"acme/widgets\".")
		return
	}
	if req.GitHubScope != store.ScopeRepository && req.GitHubScope != store.ScopeOrganization {
		s.fail(w, r, http.StatusBadRequest, "invalid_request",
			"github_scope must be \"repository\" or \"organization\".", "")
		return
	}

	// Redeeming first means a machine that fails validation has not burned a
	// single-use token, and one that passes cannot double-spend it.
	if _, err := s.store.RedeemEnrollmentToken(r.Context(), token); err != nil {
		switch {
		case errors.Is(err, store.ErrTokenInvalid):
			s.fail(w, r, http.StatusUnauthorized, "invalid_credentials",
				"The enrollment token was not accepted.",
				"Create a new one with `runnerly server enrollment-token create`.")
		case errors.Is(err, store.ErrTokenExhausted):
			s.fail(w, r, http.StatusForbidden, "token_exhausted",
				"That enrollment token has expired, been revoked, or been used as many times as allowed.",
				"Create a new one with `runnerly server enrollment-token create`.")
		default:
			s.failInternal(w, r, err, "redeem enrollment token")
		}
		return
	}

	runner, err := s.store.UpsertRunner(r.Context(), store.RegisterRunner{
		Name:          req.Name,
		GitHubHost:    req.GitHubHost,
		GitHubScope:   req.GitHubScope,
		GitHubScopeID: req.GitHubScopeID,
		OS:            req.OS,
		Architecture:  req.Architecture,
		CPUCount:      req.CPUCount,
		MemoryBytes:   req.MemoryBytes,
		DiskBytes:     req.DiskBytes,
		Labels:        req.Labels,
		Ephemeral:     req.Ephemeral,
		RunnerVersion: req.RunnerVersion,
		AgentVersion:  req.AgentVersion,
	})
	if err != nil {
		s.failInternal(w, r, err, "register runner")
		return
	}

	machineToken, err := s.store.IssueMachineToken(r.Context(), runner.ID)
	if err != nil {
		s.failInternal(w, r, err, "issue machine token")
		return
	}

	if _, err := s.store.RecordEvent(r.Context(), store.NewEvent{
		RunnerID: runner.ID,
		Event:    "runner_enrolled",
		Severity: store.SeverityInfo,
		Message:  "the machine enrolled with the control plane",
		Data:     map[string]any{"agent_version": req.AgentVersion},
	}); err != nil {
		// The enrollment worked; losing its event is not worth failing it.
		s.log(r).Warn("could not record the enrollment event",
			"event", "event_write_failed", "error", err.Error())
	}

	s.log(r).Info("runner enrolled",
		"event", "runner_enrolled", "runner", runner.Name, "runner_id", runner.ID)

	s.writeJSON(w, r, http.StatusCreated, RegisterResponse{
		Runner:       runner,
		MachineToken: machineToken,
		Config:       s.agentConfig(),
	})
}

// HeartbeatRequest is one report from an agent.
type HeartbeatRequest struct {
	Status        string  `json:"status"`
	StatusDetail  string  `json:"status_detail"`
	CPUPercent    float64 `json:"cpu"`
	MemoryPercent float64 `json:"memory"`
	DiskPercent   float64 `json:"disk"`
	RunnerVersion string  `json:"runner_version"`
	AgentVersion  string  `json:"agent_version"`
}

// PendingCommand is an instruction handed to the agent.
type PendingCommand struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

// HeartbeatResponse tells the agent what the server now believes, and hands
// over anything an operator has asked it to do.
//
// Commands ride on the heartbeat because the server cannot reach an agent:
// runner machines sit behind NAT and firewalls, and opening an inbound port
// on each one would be worse than waiting for the next beat.
type HeartbeatResponse struct {
	Runner   store.Runner     `json:"runner"`
	Config   AgentConfig      `json:"config"`
	Commands []PendingCommand `json:"commands,omitempty"`
	// MachineToken is a replacement credential, sent when the current one
	// is old enough to rotate. The agent stores it and uses it from the
	// next request; the previous one stops working immediately.
	MachineToken string `json:"machine_token,omitempty"`
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	runner, _ := runnerFrom(r.Context())

	var req HeartbeatRequest
	if !s.decode(w, r, &req) {
		return
	}
	if req.Status != "" && !validStatus(req.Status) {
		s.fail(w, r, http.StatusBadRequest, "invalid_request",
			"status must be one of offline, starting, online, busy, stopping or error.", "")
		return
	}

	updated, err := s.store.RecordHeartbeat(r.Context(), runner.ID, store.Heartbeat{
		Status:        req.Status,
		StatusDetail:  req.StatusDetail,
		CPUPercent:    req.CPUPercent,
		MemoryPercent: req.MemoryPercent,
		DiskPercent:   req.DiskPercent,
		RunnerVersion: req.RunnerVersion,
		AgentVersion:  req.AgentVersion,
	})
	if err != nil {
		s.failStore(w, r, err, "the runner")
		return
	}

	s.heartbeats.Inc("")

	commands, err := s.store.TakeCommands(r.Context(), runner.ID)
	if err != nil {
		// The heartbeat itself worked; failing it now would make the runner
		// look offline over a queue problem.
		s.log(r).Warn("could not read pending commands",
			"event", "commands_read_failed", "error", err.Error())
	}

	pending := make([]PendingCommand, 0, len(commands))
	for _, c := range commands {
		pending = append(pending, PendingCommand{ID: c.ID, Command: c.Command})
	}

	// Rotate the credential if it has been in use long enough, so a leaked
	// token is worth something for a day rather than for ever.
	rotated, didRotate, err := s.store.RotateMachineTokenIfOld(r.Context(), runner.ID, s.tokenLifetime)
	if err != nil {
		// The heartbeat itself worked. Failing it over a rotation problem
		// would make the runner look offline.
		s.log(r).Warn("could not rotate the machine token",
			"event", "rotation_failed", "error", err.Error())
	}
	if didRotate {
		s.log(r).Info("rotated the machine token",
			"event", "token_rotated", "runner", runner.Name)
		if _, err := s.store.RecordEvent(r.Context(), store.NewEvent{
			RunnerID: runner.ID,
			Event:    "machine_token_rotated",
			Severity: store.SeverityInfo,
			Message:  "the agent was issued a new credential",
		}); err != nil {
			s.log(r).Warn("could not record the rotation event",
				"event", "event_write_failed", "error", err.Error())
		}
	}

	s.writeJSON(w, r, http.StatusOK, HeartbeatResponse{
		Runner:       updated,
		Config:       s.agentConfig(),
		Commands:     pending,
		MachineToken: rotated,
	})
}

// CommandResultRequest is what an agent reports after trying a command.
type CommandResultRequest struct {
	// Error is empty when the command succeeded.
	Error string `json:"error"`
}

func (s *Server) handleAgentCommandResult(w http.ResponseWriter, r *http.Request) {
	runner, _ := runnerFrom(r.Context())

	var req CommandResultRequest
	if !s.decode(w, r, &req) {
		return
	}

	// The runner id is part of the lookup, so one agent cannot close
	// another's command by guessing an id.
	if err := s.store.CompleteCommand(r.Context(), runner.ID, r.PathValue("id"), req.Error); err != nil {
		s.failStore(w, r, err, "that command for this runner")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"status": "recorded"})
}

// EventsRequest is a batch of things that happened between heartbeats.
type EventsRequest struct {
	Events []AgentEvent `json:"events"`
}

// AgentEvent is one reported event.
type AgentEvent struct {
	Event    string         `json:"event"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	Data     map[string]any `json:"data"`
}

// EventsResponse says how many were stored.
type EventsResponse struct {
	Stored int `json:"stored"`
}

// maxEventsPerRequest bounds a batch so one agent cannot fill the table in a
// single call.
const maxEventsPerRequest = 200

func (s *Server) handleAgentEvents(w http.ResponseWriter, r *http.Request) {
	runner, _ := runnerFrom(r.Context())

	var req EventsRequest
	if !s.decode(w, r, &req) {
		return
	}
	if len(req.Events) > maxEventsPerRequest {
		s.fail(w, r, http.StatusBadRequest, "too_many_events",
			"A batch may hold at most 200 events.", "Send them in several requests.")
		return
	}

	batch := make([]store.NewEvent, 0, len(req.Events))
	for _, e := range req.Events {
		if e.Severity != "" && !validSeverity(e.Severity) {
			s.fail(w, r, http.StatusBadRequest, "invalid_request",
				"severity must be one of debug, info, warn or error.", "")
			return
		}
		batch = append(batch, store.NewEvent{
			// An agent may only write events about its own runner, whatever
			// it puts in the body.
			RunnerID: runner.ID,
			Event:    e.Event,
			Severity: e.Severity,
			Message:  e.Message,
			Data:     e.Data,
		})
	}

	// Count what the agents are telling us before storing it, so the
	// metrics reflect what was reported even if the write fails.
	for _, e := range batch {
		switch {
		case e.Severity == store.SeverityError:
			s.agentErrors.Inc("")
		case e.Event == "runner_restarting":
			s.restarts.Inc("")
		}
	}

	stored, err := s.store.RecordEvents(r.Context(), batch)
	if err != nil {
		s.failInternal(w, r, err, "record events")
		return
	}
	s.writeJSON(w, r, http.StatusOK, EventsResponse{Stored: stored})
}

// RetireRequest is an agent saying a runner has finished for good.
type RetireRequest struct {
	// Reason is why, for the dashboard: "ran its job", "stopped without
	// running a job", and so on.
	Reason string `json:"reason"`
}

// handleAgentRetire marks a runner finished.
//
// An ephemeral runner is taken apart when its job ends, and without this the
// control plane would only see the heartbeats stop — indistinguishable from
// a machine that fell over. A dashboard full of "offline" rows that are
// actually successes is worse than no dashboard.
func (s *Server) handleAgentRetire(w http.ResponseWriter, r *http.Request) {
	runner, _ := runnerFrom(r.Context())

	var req RetireRequest
	if !s.decode(w, r, &req) {
		return
	}

	retired, err := s.store.RetireRunner(r.Context(), runner.ID, req.Reason)
	if err != nil {
		s.failStore(w, r, err, "that runner")
		return
	}

	if _, err := s.store.RecordEvent(r.Context(), store.NewEvent{
		RunnerID: runner.ID,
		Event:    "runner_retired",
		Severity: store.SeverityInfo,
		Message:  req.Reason,
	}); err != nil {
		s.log(r).Warn("could not record the retirement event",
			"event", "event_write_failed", "error", err.Error())
	}
	s.log(r).Info("runner retired",
		"event", "runner_retired", "runner", retired.Name, "reason", req.Reason)

	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"retired": retired.Name,
		"at":      retired.RetiredAt,
	})
}

func (s *Server) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	runner, _ := runnerFrom(r.Context())
	s.writeJSON(w, r, http.StatusOK, struct {
		Runner store.Runner `json:"runner"`
		Config AgentConfig  `json:"config"`
	}{Runner: runner, Config: s.agentConfig()})
}

func (s *Server) agentConfig() AgentConfig {
	return AgentConfig{HeartbeatIntervalSeconds: int(s.heartbeatInterval / time.Second)}
}

func validStatus(status string) bool {
	switch status {
	case store.StatusOffline, store.StatusStarting, store.StatusOnline,
		store.StatusBusy, store.StatusStopping, store.StatusError:
		return true
	}
	return false
}

func validSeverity(severity string) bool {
	switch severity {
	case store.SeverityDebug, store.SeverityInfo, store.SeverityWarn, store.SeverityError:
		return true
	}
	return false
}
