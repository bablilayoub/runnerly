package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Viewer returns the account the token belongs to. It is the cheapest way to
// check that a token works.
func (c *Client) Viewer(ctx context.Context) (*User, error) {
	var user User
	if _, err := c.do(ctx, http.MethodGet, "user", nil, &user); err != nil {
		return nil, fmt.Errorf("identify token: %w", err)
	}
	return &user, nil
}

// Identity is who a token belongs to and what GitHub says it can do.
type Identity struct {
	User *User `json:"user"`
	// Scopes is empty for fine-grained personal access tokens, which do not
	// report scopes. That is not an error, and callers must not treat an
	// empty list as "no permissions".
	Scopes []string `json:"scopes"`
	// FineGrained is true when GitHub reported no scopes at all, which is how
	// a fine-grained token looks.
	FineGrained bool `json:"fine_grained"`
}

// Identify returns the account a token belongs to and its scopes in one
// request. It is the cheapest way to check that a token works.
func (c *Client) Identify(ctx context.Context) (*Identity, error) {
	var user User
	header, err := c.do(ctx, http.MethodGet, "user", nil, &user)
	if err != nil {
		return nil, fmt.Errorf("identify token: %w", err)
	}
	scopes := splitHeaderList(header.Get("X-OAuth-Scopes"))
	return &Identity{User: &user, Scopes: scopes, FineGrained: len(scopes) == 0}, nil
}

// RequiredScope is the classic-PAT scope needed to administer runners at a
// scope kind.
func RequiredScope(kind ScopeKind) string {
	if kind == KindOrganization {
		return "admin:org"
	}
	return "repo"
}

// MissingScope returns the scope a classic token is missing to administer
// runners at kind, or "" if nothing is missing.
//
// It returns "" for a fine-grained token: GitHub does not report scopes for
// those, so absence of a scope proves nothing and guessing would produce a
// false alarm. Such a token fails at the point of use with a 403 that says
// what it needs.
func MissingScope(kind ScopeKind, have []string) string {
	if len(have) == 0 {
		return ""
	}
	want := RequiredScope(kind)
	for _, s := range have {
		if s == want {
			return ""
		}
	}
	return want
}

// ErrNoRelease means the repository has published none.
var ErrNoRelease = errors.New("no releases have been published")

// LatestRelease returns the tag of a repository's newest release.
//
// GitHub answers 404 when a repository has never published one, which is
// not the same as the repository being missing; the caller needs to tell
// those apart to say "nothing to compare against" rather than "not found".
func (c *Client) LatestRelease(ctx context.Context, owner, repo string) (string, error) {
	var release struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}
	path := "repos/" + owner + "/" + repo + "/releases/latest"
	if _, err := c.do(ctx, http.MethodGet, path, nil, &release); err != nil {
		if IsNotFound(err) {
			return "", ErrNoRelease
		}
		return "", fmt.Errorf("look up the latest release of %s/%s: %w", owner, repo, err)
	}
	if release.TagName == "" {
		return "", ErrNoRelease
	}
	return release.TagName, nil
}

// Repository looks up a single repository.
func (c *Client) Repository(ctx context.Context, scope Scope) (*Repository, error) {
	if scope.Kind != KindRepository {
		return nil, fmt.Errorf("Repository needs a repository scope, got %s", scope.Kind)
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var repo Repository
	path := "repos/" + scope.Owner + "/" + scope.Repo
	if _, err := c.do(ctx, http.MethodGet, path, nil, &repo); err != nil {
		return nil, fmt.Errorf("look up %s: %w", scope, err)
	}
	return &repo, nil
}

// ListRepositoriesOptions filters repository discovery.
type ListRepositoriesOptions struct {
	// Limit caps how many repositories are returned. Zero means DefaultLimit.
	Limit int
	// AdminOnly keeps only repositories the token can administer, which is
	// what registering a runner requires.
	AdminOnly bool
}

// DefaultRepositoryLimit bounds discovery so an account with thousands of
// repositories does not make the CLI appear to hang.
const DefaultRepositoryLimit = 200

// ListRepositories returns repositories the token can see, most recently
// pushed first.
func (c *Client) ListRepositories(ctx context.Context, opts ListRepositoriesOptions) ([]Repository, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultRepositoryLimit
	}

	var out []Repository
	for page := 1; page <= maxPages && len(out) < limit; page++ {
		var batch []Repository
		path := "user/repos?sort=pushed&" + runnerQuery(page)
		if _, err := c.do(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, fmt.Errorf("list repositories: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			if opts.AdminOnly && !r.Permissions.Admin {
				continue
			}
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
		if len(batch) < perPage {
			break
		}
	}
	return out, nil
}

// SearchRepositories filters the visible repositories by a case-insensitive
// substring of the full name. It is a local filter, not GitHub's search API,
// because discovery only ever needs to narrow a list the operator can already
// see.
func (c *Client) SearchRepositories(ctx context.Context, query string, opts ListRepositoriesOptions) ([]Repository, error) {
	repos, err := c.ListRepositories(ctx, opts)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return repos, nil
	}
	var out []Repository
	for _, r := range repos {
		if strings.Contains(strings.ToLower(r.FullName), query) {
			out = append(out, r)
		}
	}
	return out, nil
}
