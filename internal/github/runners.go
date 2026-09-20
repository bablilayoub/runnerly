package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ErrRunnerNotFound is returned when no runner matches.
var ErrRunnerNotFound = errors.New("no such runner")

// perPage is GitHub's maximum page size for these endpoints.
const perPage = 100

// maxPages bounds pagination so a server that never advances cannot spin
// forever.
const maxPages = 100

// ListRunners returns every self-hosted runner registered at the scope.
func (c *Client) ListRunners(ctx context.Context, scope Scope) ([]Runner, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}

	var all []Runner
	for page := 1; page <= maxPages; page++ {
		var body struct {
			TotalCount int      `json:"total_count"`
			Runners    []Runner `json:"runners"`
		}
		path := fmt.Sprintf("%s/actions/runners?per_page=%d&page=%d", scope.apiPath(), perPage, page)
		if _, err := c.do(ctx, http.MethodGet, path, nil, &body); err != nil {
			return nil, fmt.Errorf("list runners for %s: %w", scope, err)
		}

		all = append(all, body.Runners...)
		if len(body.Runners) == 0 || len(all) >= body.TotalCount {
			break
		}
	}
	return all, nil
}

// GetRunner returns one runner by its GitHub ID.
func (c *Client) GetRunner(ctx context.Context, scope Scope, id int64) (*Runner, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var runner Runner
	path := scope.apiPath() + "/actions/runners/" + strconv.FormatInt(id, 10)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &runner); err != nil {
		return nil, fmt.Errorf("get runner %d in %s: %w", id, scope, err)
	}
	return &runner, nil
}

// FindRunnerByName returns the runner with the given name. Names are unique
// within a scope, so at most one can match. It returns ErrRunnerNotFound when
// nothing matches.
func (c *Client) FindRunnerByName(ctx context.Context, scope Scope, name string) (*Runner, error) {
	runners, err := c.ListRunners(ctx, scope)
	if err != nil {
		return nil, err
	}
	for i := range runners {
		if strings.EqualFold(runners[i].Name, name) {
			return &runners[i], nil
		}
	}
	return nil, fmt.Errorf("%q in %s: %w", name, scope, ErrRunnerNotFound)
}

// DeleteRunner removes a runner's registration from GitHub.
//
// This does not stop a runner process that is still running on a machine; it
// only removes the registration. A running runner that has been deleted will
// fail to reconnect.
func (c *Client) DeleteRunner(ctx context.Context, scope Scope, id int64) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	path := scope.apiPath() + "/actions/runners/" + strconv.FormatInt(id, 10)
	if _, err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("delete runner %d in %s: %w", id, scope, err)
	}
	return nil
}

// CreateRegistrationToken returns a short-lived token used to register a new
// runner. GitHub expires these one hour after issue, which is why Runnerly
// requests one per registration rather than storing it.
func (c *Client) CreateRegistrationToken(ctx context.Context, scope Scope) (*RegistrationToken, error) {
	return c.createToken(ctx, scope, "registration-token")
}

// CreateRemoveToken returns a short-lived token used to deregister a runner
// from the machine it runs on.
func (c *Client) CreateRemoveToken(ctx context.Context, scope Scope) (*RegistrationToken, error) {
	return c.createToken(ctx, scope, "remove-token")
}

func (c *Client) createToken(ctx context.Context, scope Scope, kind string) (*RegistrationToken, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var token RegistrationToken
	path := scope.apiPath() + "/actions/runners/" + kind
	if _, err := c.do(ctx, http.MethodPost, path, nil, &token); err != nil {
		return nil, fmt.Errorf("create %s for %s: %w", kind, scope, err)
	}
	if token.Token == "" {
		return nil, fmt.Errorf("create %s for %s: GitHub returned an empty token", kind, scope)
	}
	return &token, nil
}

// ListDownloads returns the official runner releases GitHub offers for this
// scope, including the SHA-256 checksum of each archive.
//
// Runnerly uses this rather than hard-coding a runner version, so a machine
// always gets the release GitHub currently expects.
func (c *Client) ListDownloads(ctx context.Context, scope Scope) ([]Download, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var downloads []Download
	path := scope.apiPath() + "/actions/runners/downloads"
	if _, err := c.do(ctx, http.MethodGet, path, nil, &downloads); err != nil {
		return nil, fmt.Errorf("list runner downloads for %s: %w", scope, err)
	}
	return downloads, nil
}

// FindDownload returns the release matching a GitHub platform name, for
// example ("linux", "x64"). Callers convert from Go's GOOS/GOARCH first.
func (c *Client) FindDownload(ctx context.Context, scope Scope, os, arch string) (*Download, error) {
	downloads, err := c.ListDownloads(ctx, scope)
	if err != nil {
		return nil, err
	}
	for i := range downloads {
		if downloads[i].OS == os && downloads[i].Architecture == arch {
			return &downloads[i], nil
		}
	}

	available := make([]string, 0, len(downloads))
	for _, d := range downloads {
		available = append(available, d.OS+"/"+d.Architecture)
	}
	return nil, fmt.Errorf("GitHub publishes no Actions runner for %s/%s.\nIt offers: %s",
		os, arch, strings.Join(available, ", "))
}

// runnerQuery is used by callers that need to build their own paged request.
func runnerQuery(page int) string {
	v := url.Values{}
	v.Set("per_page", strconv.Itoa(perPage))
	v.Set("page", strconv.Itoa(page))
	return v.Encode()
}
