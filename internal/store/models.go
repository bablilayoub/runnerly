package store

import "time"

// Runner statuses, as reported by an agent.
const (
	StatusOffline  = "offline"
	StatusStarting = "starting"
	StatusOnline   = "online"
	StatusBusy     = "busy"
	StatusStopping = "stopping"
	StatusError    = "error"
	// StatusRetired is reported for a runner that finished on purpose, so
	// it is not confused with one whose machine stopped answering.
	StatusRetired = "retired"
)

// Scope kinds, mirroring github.ScopeKind without importing it: the store
// speaks to the database, not to GitHub.
const (
	ScopeRepository   = "repository"
	ScopeOrganization = "organization"
)

// Health is how fresh a runner's last heartbeat is.
type Health string

const (
	// HealthOK means a heartbeat arrived recently.
	HealthOK Health = "healthy"
	// HealthStale means heartbeats have slowed but not stopped.
	HealthStale Health = "stale"
	// HealthOffline means nothing has been heard for too long.
	HealthOffline Health = "offline"
)

// Runner is a machine the control plane knows about.
type Runner struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	GitHubHost     string     `json:"github_host"`
	GitHubScope    string     `json:"github_scope"`
	GitHubScopeID  string     `json:"github_scope_id"`
	GitHubRunnerID *int64     `json:"github_runner_id,omitempty"`
	Status         string     `json:"status"`
	StatusDetail   string     `json:"status_detail,omitempty"`
	OS             string     `json:"os"`
	Architecture   string     `json:"architecture"`
	CPUCount       int        `json:"cpu_count"`
	MemoryBytes    int64      `json:"memory_bytes"`
	DiskBytes      int64      `json:"disk_bytes"`
	Labels         []string   `json:"labels"`
	Ephemeral      bool       `json:"ephemeral"`
	RunnerVersion  string     `json:"runner_version"`
	AgentVersion   string     `json:"agent_version"`
	CPUPercent     float64    `json:"cpu_percent"`
	MemoryPercent  float64    `json:"memory_percent"`
	DiskPercent    float64    `json:"disk_percent"`
	LastHeartbeat  *time.Time `json:"last_heartbeat,omitempty"`
	// RetiredAt is set when a runner finished for good, which for an
	// ephemeral runner means it ran its job and was taken apart.
	RetiredAt     *time.Time `json:"retired_at,omitempty"`
	RetiredReason string     `json:"retired_reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Retired reports whether the runner has finished for good.
func (r Runner) Retired() bool { return r.RetiredAt != nil }

// Thresholds decide when a runner counts as stale or offline. The project
// plan requires these to be configurable rather than baked in.
type Thresholds struct {
	// Stale is how long a heartbeat may be missing before a runner is
	// reported as stale.
	Stale time.Duration
	// Offline is how long before it is reported as offline.
	Offline time.Duration
}

// DefaultThresholds is the plan's schedule: healthy under 30s, stale to 90s,
// offline beyond that.
func DefaultThresholds() Thresholds {
	return Thresholds{Stale: 30 * time.Second, Offline: 90 * time.Second}
}

// Health reports how fresh the runner's heartbeat is, as of now.
//
// This is computed on read rather than written into the row, because a runner
// that stops sending heartbeats never gets the chance to tell us so. Nothing
// would update a stored value at the moment it became wrong.
func (r Runner) Health(now time.Time, t Thresholds) Health {
	if r.Retired() || r.LastHeartbeat == nil {
		return HealthOffline
	}
	switch age := now.Sub(*r.LastHeartbeat); {
	case age >= t.Offline:
		return HealthOffline
	case age >= t.Stale:
		return HealthStale
	default:
		return HealthOK
	}
}

// EffectiveStatus combines what the agent last reported with how long ago it
// said it. A runner that claims to be online but stopped reporting is
// offline, whatever the column says.
func (r Runner) EffectiveStatus(now time.Time, t Thresholds) string {
	// A retired runner stopped on purpose. Reporting it as offline would
	// make a fleet of finished ephemeral runners look like a fleet of
	// broken ones.
	if r.Retired() {
		return StatusRetired
	}
	if r.Health(now, t) == HealthOffline {
		return StatusOffline
	}
	return r.Status
}

// Event severities.
const (
	SeverityDebug = "debug"
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"
)

// Event is something that happened to a runner.
type Event struct {
	ID        int64          `json:"id"`
	RunnerID  *string        `json:"runner_id,omitempty"`
	Event     string         `json:"event"`
	Severity  string         `json:"severity"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// EnrollmentToken lets a machine enroll itself. The secret is returned only
// when the token is created; afterwards only its hash exists.
type EnrollmentToken struct {
	ID          string     `json:"id"`
	Description string     `json:"description,omitempty"`
	MaxUses     *int       `json:"max_uses,omitempty"`
	Uses        int        `json:"uses"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Usable reports whether the token may still enroll a machine.
func (t EnrollmentToken) Usable(now time.Time) bool {
	switch {
	case t.RevokedAt != nil:
		return false
	case t.ExpiresAt != nil && !now.Before(*t.ExpiresAt):
		return false
	case t.MaxUses != nil && t.Uses >= *t.MaxUses:
		return false
	default:
		return true
	}
}

// User is a dashboard account.
type User struct {
	ID          string    `json:"id"`
	GitHubID    int64     `json:"github_id"`
	Login       string    `json:"login"`
	Name        string    `json:"name,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}
