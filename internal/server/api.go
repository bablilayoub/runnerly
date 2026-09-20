package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/bablilayoub/runnerly/internal/store"
)

// RunnerView is a runner as the dashboard sees it: the stored row plus what
// the server concludes from how long ago it reported.
type RunnerView struct {
	store.Runner
	// Status here overrides the stored column with the effective one, so a
	// client never has to redo the freshness arithmetic.
	Status string       `json:"status"`
	Health store.Health `json:"health"`
	// LastHeartbeatAge is how long ago the runner reported, in seconds. Null
	// when it never has.
	LastHeartbeatAge *float64 `json:"last_heartbeat_age_seconds"`
}

func (s *Server) view(r store.Runner) RunnerView {
	now := s.now()
	v := RunnerView{
		Runner: r,
		Status: r.EffectiveStatus(now, s.thresholds),
		Health: r.Health(now, s.thresholds),
	}
	if r.LastHeartbeat != nil {
		age := now.Sub(*r.LastHeartbeat).Seconds()
		v.LastHeartbeatAge = &age
	}
	return v
}

// OverviewResponse is the dashboard's landing page.
type OverviewResponse struct {
	Runners store.Counts  `json:"runners"`
	Events  []store.Event `json:"recent_events"`
	// Thresholds are echoed so a client can label a runner the same way the
	// server does.
	Thresholds ThresholdsView `json:"thresholds"`
}

// ThresholdsView reports the freshness thresholds in seconds.
type ThresholdsView struct {
	StaleAfterSeconds   int `json:"stale_after_seconds"`
	OfflineAfterSeconds int `json:"offline_after_seconds"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	counts, err := s.store.CountRunners(r.Context(), s.now(), s.thresholds)
	if err != nil {
		s.failInternal(w, r, err, "count runners")
		return
	}
	events, err := s.store.ListEvents(r.Context(), store.ListEventsOptions{Limit: 20})
	if err != nil {
		s.failInternal(w, r, err, "list events")
		return
	}

	s.writeJSON(w, r, http.StatusOK, OverviewResponse{
		Runners: counts,
		Events:  events,
		Thresholds: ThresholdsView{
			StaleAfterSeconds:   int(s.thresholds.Stale / time.Second),
			OfflineAfterSeconds: int(s.thresholds.Offline / time.Second),
		},
	})
}

// RunnersResponse is a runner listing.
type RunnersResponse struct {
	Runners []RunnerView `json:"runners"`
}

func (s *Server) handleListRunners(w http.ResponseWriter, r *http.Request) {
	limit, ok := s.intQuery(w, r, "limit", 0)
	if !ok {
		return
	}

	runners, err := s.store.ListRunners(r.Context(), store.ListRunnersOptions{
		Scope: r.URL.Query().Get("scope"),
		Limit: limit,
	})
	if err != nil {
		s.failInternal(w, r, err, "list runners")
		return
	}

	views := make([]RunnerView, 0, len(runners))
	for _, runner := range runners {
		views = append(views, s.view(runner))
	}
	s.writeJSON(w, r, http.StatusOK, RunnersResponse{Runners: views})
}

func (s *Server) handleGetRunner(w http.ResponseWriter, r *http.Request) {
	runner, err := s.store.Runner(r.Context(), r.PathValue("id"))
	if err != nil {
		s.failStore(w, r, err, "that runner")
		return
	}
	s.writeJSON(w, r, http.StatusOK, s.view(runner))
}

func (s *Server) handleDeleteRunner(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := r.PathValue("id")

	runner, err := s.store.Runner(r.Context(), id)
	if err != nil {
		s.failStore(w, r, err, "that runner")
		return
	}
	if err := s.store.DeleteRunner(r.Context(), id); err != nil {
		s.failStore(w, r, err, "that runner")
		return
	}

	if err := s.store.RecordAudit(r.Context(), user.Login, "runner.delete", runner.Name,
		map[string]any{"scope": runner.GitHubScopeID}); err != nil {
		s.log(r).Warn("could not record the audit entry",
			"event", "audit_write_failed", "error", err.Error())
	}
	s.log(r).Info("runner deleted", "event", "runner_deleted", "runner", runner.Name)

	// This removes Runnerly's record. It does not deregister the runner with
	// GitHub, and it does not touch the machine; the response says so rather
	// than leaving the caller to assume otherwise.
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"deleted": runner.Name,
		"note": "Removed from the control plane only. The runner is still registered with " +
			"GitHub and still installed on its machine; use `runnerly runner remove` there.",
	})
}

// RestartResponse describes the queued restart.
type RestartResponse struct {
	Command store.Command `json:"command"`
	Note    string        `json:"note"`
}

func (s *Server) handleRestartRunner(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := r.PathValue("id")

	runner, err := s.store.Runner(r.Context(), id)
	if err != nil {
		s.failStore(w, r, err, "that runner")
		return
	}

	command, err := s.store.QueueCommand(r.Context(), runner.ID, store.CommandRestart, user.Login)
	if err != nil {
		s.failInternal(w, r, err, "queue the restart")
		return
	}

	if err := s.store.RecordAudit(r.Context(), user.Login, "runner.restart", runner.Name, nil); err != nil {
		s.log(r).Warn("could not record the audit entry",
			"event", "audit_write_failed", "error", err.Error())
	}
	if _, err := s.store.RecordEvent(r.Context(), store.NewEvent{
		RunnerID: runner.ID,
		Event:    "restart_requested",
		Severity: store.SeverityInfo,
		Message:  user.Login + " asked for a restart",
	}); err != nil {
		s.log(r).Warn("could not record the event",
			"event", "event_write_failed", "error", err.Error())
	}

	// Be honest about the delay rather than implying the button was
	// immediate: the agent picks this up on its next heartbeat.
	s.writeJSON(w, r, http.StatusAccepted, RestartResponse{
		Command: command,
		Note: fmt.Sprintf("Queued. The agent collects it on its next heartbeat, within about %ds.",
			int(s.heartbeatInterval/time.Second)),
	})
}

// CommandsResponse lists a runner's recent commands.
type CommandsResponse struct {
	Commands []store.Command `json:"commands"`
}

func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
	limit, ok := s.intQuery(w, r, "limit", 0)
	if !ok {
		return
	}
	commands, err := s.store.ListCommands(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		s.failInternal(w, r, err, "list commands")
		return
	}
	s.writeJSON(w, r, http.StatusOK, CommandsResponse{Commands: commands})
}

// EventsListResponse is an event feed page.
type EventsListResponse struct {
	Events []store.Event `json:"events"`
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	limit, ok := s.intQuery(w, r, "limit", 0)
	if !ok {
		return
	}
	before, ok := s.intQuery(w, r, "before", 0)
	if !ok {
		return
	}

	events, err := s.store.ListEvents(r.Context(), store.ListEventsOptions{
		RunnerID: r.URL.Query().Get("runner_id"),
		Severity: r.URL.Query().Get("severity"),
		Before:   int64(before),
		Limit:    limit,
	})
	if err != nil {
		s.failInternal(w, r, err, "list events")
		return
	}
	s.writeJSON(w, r, http.StatusOK, EventsListResponse{Events: events})
}

// CreateEnrollmentTokenRequest asks for a new enrollment token.
type CreateEnrollmentTokenRequest struct {
	Description string `json:"description"`
	// MaxUses limits how many machines may enroll with it. Null means any
	// number.
	MaxUses *int `json:"max_uses"`
	// ExpiresInHours expires the token. Zero means it does not expire.
	ExpiresInHours int `json:"expires_in_hours"`
}

// CreateEnrollmentTokenResponse returns the secret, once.
type CreateEnrollmentTokenResponse struct {
	Token store.EnrollmentToken `json:"token"`
	// Secret is shown only here. The server stores only its hash.
	Secret string `json:"secret"`
	Note   string `json:"note"`
}

func (s *Server) handleCreateEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())

	var req CreateEnrollmentTokenRequest
	if !s.decode(w, r, &req) {
		return
	}
	if req.MaxUses != nil && *req.MaxUses < 1 {
		s.fail(w, r, http.StatusBadRequest, "invalid_request",
			"max_uses must be at least 1, or omitted for unlimited.", "")
		return
	}
	if req.ExpiresInHours < 0 {
		s.fail(w, r, http.StatusBadRequest, "invalid_request",
			"expires_in_hours must not be negative.", "")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresInHours > 0 {
		t := s.now().UTC().Add(time.Duration(req.ExpiresInHours) * time.Hour)
		expiresAt = &t
	}

	token, secret, err := s.store.CreateEnrollmentToken(r.Context(), req.Description, req.MaxUses, expiresAt)
	if err != nil {
		s.failInternal(w, r, err, "create enrollment token")
		return
	}
	if err := s.store.RecordAudit(r.Context(), user.Login, "enrollment_token.create", token.ID, nil); err != nil {
		s.log(r).Warn("could not record the audit entry",
			"event", "audit_write_failed", "error", err.Error())
	}

	s.writeJSON(w, r, http.StatusCreated, CreateEnrollmentTokenResponse{
		Token:  token,
		Secret: secret,
		Note:   "This is the only time the secret is shown. Runnerly stores only its hash.",
	})
}

// EnrollmentTokensResponse lists issued tokens.
type EnrollmentTokensResponse struct {
	Tokens []store.EnrollmentToken `json:"tokens"`
}

func (s *Server) handleListEnrollmentTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListEnrollmentTokens(r.Context())
	if err != nil {
		s.failInternal(w, r, err, "list enrollment tokens")
		return
	}
	s.writeJSON(w, r, http.StatusOK, EnrollmentTokensResponse{Tokens: tokens})
}

func (s *Server) handleRevokeEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id := r.PathValue("id")

	if err := s.store.RevokeEnrollmentToken(r.Context(), id); err != nil {
		s.failStore(w, r, err, "that enrollment token, or it was already revoked,")
		return
	}
	if err := s.store.RecordAudit(r.Context(), user.Login, "enrollment_token.revoke", id, nil); err != nil {
		s.log(r).Warn("could not record the audit entry",
			"event", "audit_write_failed", "error", err.Error())
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"revoked": id})
}

// intQuery reads a non-negative integer query parameter.
func (s *Server) intQuery(w http.ResponseWriter, r *http.Request, name string, fallback int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		s.fail(w, r, http.StatusBadRequest, "invalid_request",
			name+" must be a non-negative whole number.", "")
		return 0, false
	}
	return value, true
}
