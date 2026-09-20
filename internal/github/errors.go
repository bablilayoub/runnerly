package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Error is a non-2xx response from GitHub.
//
// Its message follows the project's rule for user-facing errors: what
// happened, why, and what to do next. GitHub's own message is rarely enough on
// its own — a 404 from a runner endpoint usually means "your token cannot see
// this repository", not "this repository does not exist".
type Error struct {
	Method     string
	URL        string
	StatusCode int

	// Message is GitHub's message field, if it sent one.
	Message string
	DocURL  string
	Details []ErrorDetail

	// Scopes is what the token actually has (X-OAuth-Scopes); Accepted is
	// what the endpoint wanted (X-Accepted-OAuth-Scopes). Both are empty for
	// fine-grained tokens, which do not report scopes.
	Scopes   []string
	Accepted []string

	RateLimit     RateLimit
	Authenticated bool
}

// ErrorDetail is one entry from GitHub's `errors` array.
type ErrorDetail struct {
	Resource string `json:"resource"`
	Field    string `json:"field"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func (e *Error) Error() string {
	var b strings.Builder

	summary := e.Message
	if summary == "" {
		summary = http.StatusText(e.StatusCode)
	}
	fmt.Fprintf(&b, "GitHub returned %d: %s", e.StatusCode, summary)

	for _, d := range e.Details {
		if d.Message != "" {
			fmt.Fprintf(&b, "\n  %s", d.Message)
		} else if d.Field != "" {
			fmt.Fprintf(&b, "\n  %s %s: %s", d.Resource, d.Field, d.Code)
		}
	}

	if hint := e.Hint(); hint != "" {
		b.WriteString("\n")
		b.WriteString(hint)
	}
	return b.String()
}

// Hint explains the status code in Runnerly's terms and says what to do.
func (e *Error) Hint() string {
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		if !e.Authenticated {
			return "No GitHub token was supplied.\nRun `runnerly login` or set RUNNERLY_GITHUB_TOKEN."
		}
		return "The token is invalid, expired, or was revoked.\nRun `runnerly login` to store a new one."

	case e.IsRateLimited():
		wait := "shortly"
		if !e.RateLimit.Reset.IsZero() {
			wait = "at " + e.RateLimit.Reset.Format(time.RFC3339)
		}
		if !e.Authenticated {
			return fmt.Sprintf("Unauthenticated requests are limited to 60 per hour. The limit resets %s.\n"+
				"Run `runnerly login` to raise it.", wait)
		}
		return fmt.Sprintf("The rate limit resets %s.", wait)

	case e.StatusCode == http.StatusForbidden:
		if len(e.Accepted) > 0 {
			return fmt.Sprintf("The token needs one of these scopes: %s.\nIt currently has: %s.\n"+
				"Create a new token with the required scope, then run `runnerly login`.",
				strings.Join(e.Accepted, ", "), scopeList(e.Scopes))
		}
		return "The token is valid but not permitted to do this.\n" +
			"Check that it has admin access to the repository or organization."

	case e.StatusCode == http.StatusNotFound:
		if !e.Authenticated {
			return "It may exist but be private. Run `runnerly login` and try again."
		}
		return "It does not exist, or the token cannot see it.\n" +
			"GitHub returns 404 rather than 403 for resources a token may not access, so check\n" +
			"the spelling and that the token has admin access."

	case e.StatusCode >= 500:
		return "This is a problem on GitHub's side.\nCheck https://www.githubstatus.com and retry."
	}
	return ""
}

func scopeList(scopes []string) string {
	if len(scopes) == 0 {
		return "no scopes, or it is a fine-grained token that does not report them"
	}
	return strings.Join(scopes, ", ")
}

// IsRateLimited reports whether the request was refused for rate limiting
// rather than permissions. GitHub uses 403 and 429 for both.
func (e *Error) IsRateLimited() bool {
	if e.StatusCode != http.StatusForbidden && e.StatusCode != http.StatusTooManyRequests {
		return false
	}
	if e.RateLimit.Limit > 0 && e.RateLimit.Remaining == 0 {
		return true
	}
	return strings.Contains(strings.ToLower(e.Message), "rate limit")
}

// IsNotFound reports whether err is a 404 from GitHub.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsUnauthorized reports whether err is a 401 from GitHub, meaning the token
// is missing, invalid or expired.
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsForbidden reports whether err is a 403 from GitHub.
func IsForbidden(err error) bool { return hasStatus(err, http.StatusForbidden) }

// IsRateLimited reports whether err is a rate limit refusal.
func IsRateLimited(err error) bool {
	var ghErr *Error
	return errors.As(err, &ghErr) && ghErr.IsRateLimited()
}

func hasStatus(err error, code int) bool {
	var ghErr *Error
	return errors.As(err, &ghErr) && ghErr.StatusCode == code
}

// newError builds an *Error from a failed response. The body has not been read
// yet; newError consumes it.
func (c *Client) newError(resp *http.Response) *Error {
	e := &Error{
		Method:        resp.Request.Method,
		URL:           resp.Request.URL.String(),
		StatusCode:    resp.StatusCode,
		Scopes:        splitHeaderList(resp.Header.Get("X-OAuth-Scopes")),
		Accepted:      splitHeaderList(resp.Header.Get("X-Accepted-OAuth-Scopes")),
		RateLimit:     parseRateLimit(resp.Header),
		Authenticated: c.authenticated(),
	}

	var payload struct {
		Message string        `json:"message"`
		DocURL  string        `json:"documentation_url"`
		Errors  []ErrorDetail `json:"errors"`
	}
	if body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody)); err == nil {
		if json.Unmarshal(body, &payload) == nil {
			e.Message = payload.Message
			e.DocURL = payload.DocURL
			e.Details = payload.Errors
		}
	}
	return e
}

func splitHeaderList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
