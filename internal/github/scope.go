package github

import (
	"fmt"
	"regexp"
	"strings"
)

// ScopeKind is the level a runner is registered at. Runnerly supports
// repository and organization runners; enterprise runners are not implemented.
type ScopeKind string

const (
	// KindRepository registers a runner against a single repository.
	KindRepository ScopeKind = "repository"
	// KindOrganization registers a runner available to an organization.
	KindOrganization ScopeKind = "organization"
)

// Scope identifies where a runner lives.
type Scope struct {
	Kind  ScopeKind `json:"kind"`
	Owner string    `json:"owner"`
	// Repo is empty for an organization scope.
	Repo string `json:"repo,omitempty"`
}

// GitHub allows letters, digits and hyphens in an owner login, and adds
// underscore and dot for repository names.
var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// ParseRepository builds a repository scope from an "owner/repo" string. It
// tolerates a full GitHub URL, because that is what people paste.
func ParseRepository(s string) (Scope, error) {
	original := s
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".git")

	// Accept https://github.com/owner/repo and git@github.com:owner/repo.
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			s = rest[j+1:]
		}
	} else if i := strings.Index(s, ":"); i >= 0 && strings.Contains(s[:i], "@") {
		s = s[i+1:]
	}
	s = strings.Trim(s, "/")

	owner, repo, ok := strings.Cut(s, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return Scope{}, fmt.Errorf("%q is not a repository.\nUse the owner/repo form, for example bablilayoub/example", original)
	}
	scope := Scope{Kind: KindRepository, Owner: owner, Repo: repo}
	if err := scope.Validate(); err != nil {
		return Scope{}, err
	}
	return scope, nil
}

// ForOrganization builds an organization scope.
func ForOrganization(login string) Scope {
	return Scope{Kind: KindOrganization, Owner: strings.TrimSpace(login)}
}

// Validate reports whether the scope is well formed. It does not contact
// GitHub.
func (s Scope) Validate() error {
	switch s.Kind {
	case KindRepository:
		if !ownerPattern.MatchString(s.Owner) {
			return fmt.Errorf("%q is not a valid GitHub owner", s.Owner)
		}
		if !repoPattern.MatchString(s.Repo) {
			return fmt.Errorf("%q is not a valid repository name", s.Repo)
		}
	case KindOrganization:
		if !ownerPattern.MatchString(s.Owner) {
			return fmt.Errorf("%q is not a valid GitHub organization", s.Owner)
		}
		if s.Repo != "" {
			return fmt.Errorf("an organization scope must not name a repository, got %q", s.Repo)
		}
	default:
		return fmt.Errorf("%q is not a scope kind (use %q or %q)", s.Kind, KindRepository, KindOrganization)
	}
	return nil
}

// String returns "owner/repo" for a repository and "owner" for an
// organization.
func (s Scope) String() string {
	if s.Kind == KindRepository {
		return s.Owner + "/" + s.Repo
	}
	return s.Owner
}

// apiPath is the REST path prefix for this scope's runner endpoints.
func (s Scope) apiPath() string {
	if s.Kind == KindRepository {
		return "repos/" + s.Owner + "/" + s.Repo
	}
	return "orgs/" + s.Owner
}

// WebURL is the browser-facing URL for the scope. The official runner's
// config.sh takes this, not an API URL.
func (s Scope) WebURL(host string) string {
	return WebURL(host) + "/" + s.String()
}
