package github

import "time"

// User is a GitHub account.
type User struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	// AvatarURL is shown beside the account in the dashboard. The column
	// and the component for it existed long before anything read this.
	AvatarURL string `json:"avatar_url"`
}

// Repository is the subset of a repository Runnerly cares about.
type Repository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	Archived bool   `json:"archived"`
	Fork     bool   `json:"fork"`
	HTMLURL  string `json:"html_url"`
	Owner    struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"owner"`
	Permissions struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
		Pull  bool `json:"pull"`
	} `json:"permissions"`
}

// Scope returns the runner scope for this repository.
func (r Repository) Scope() Scope {
	return Scope{Kind: KindRepository, Owner: r.Owner.Login, Repo: r.Name}
}

// Label is a runner label. GitHub distinguishes labels it applies itself
// ("read-only") from ones the operator chose ("custom").
type Label struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Runner statuses as reported by GitHub.
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
)

// Runner is a self-hosted runner registered with GitHub.
type Runner struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	OS     string  `json:"os"`
	Status string  `json:"status"`
	Busy   bool    `json:"busy"`
	Labels []Label `json:"labels"`
	// Ephemeral is reported by newer GitHub versions; older ones omit it.
	Ephemeral bool `json:"ephemeral"`
}

// LabelNames returns the runner's labels as plain strings, in the order
// GitHub reported them.
func (r Runner) LabelNames() []string {
	names := make([]string, 0, len(r.Labels))
	for _, l := range r.Labels {
		names = append(names, l.Name)
	}
	return names
}

// State summarizes status and busy into one word for display.
func (r Runner) State() string {
	switch {
	case r.Status != StatusOnline:
		return StatusOffline
	case r.Busy:
		return "busy"
	default:
		return StatusOnline
	}
}

// RegistrationToken is a short-lived token used to add or remove a runner.
// GitHub expires these an hour after issue.
type RegistrationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Expired reports whether the token is past its expiry.
func (t RegistrationToken) Expired(now time.Time) bool {
	return !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt)
}

// Download describes a release of the official actions/runner for one
// platform, including the checksum needed to verify it.
type Download struct {
	OS             string `json:"os"`
	Architecture   string `json:"architecture"`
	DownloadURL    string `json:"download_url"`
	Filename       string `json:"filename"`
	SHA256Checksum string `json:"sha256_checksum"`
}
