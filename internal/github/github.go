// Package github is a small client for the parts of the GitHub REST API that
// Runnerly needs: identifying a token, finding repositories, and managing
// self-hosted runners.
//
// It is deliberately not a general-purpose GitHub library. Six endpoints do
// not justify a dependency, and a narrow client lets every error carry a
// message an operator can act on.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// apiVersion pins the REST API behavior. GitHub uses it to keep responses
// stable across breaking changes.
const apiVersion = "2022-11-28"

// DefaultUserAgent identifies Runnerly to GitHub.
const DefaultUserAgent = "runnerly"

// maxErrorBody bounds how much of an error response we read before giving up.
const maxErrorBody = 64 << 10

// Client talks to one GitHub deployment.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	token      string
	userAgent  string
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client, for timeouts, proxies or tests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithBaseURL points the client at a different API root. Use it for GitHub
// Enterprise Server and in tests.
func WithBaseURL(raw string) Option {
	return func(c *Client) {
		if u, err := url.Parse(strings.TrimRight(raw, "/")); err == nil {
			c.baseURL = u
		}
	}
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// New returns a client authenticated with token. An empty token is allowed;
// unauthenticated requests are heavily rate limited and cannot see private
// repositories, which the resulting errors say.
func New(token string, opts ...Option) *Client {
	base, _ := url.Parse(APIBaseURL(""))
	c := &Client{
		baseURL:    base,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		userAgent:  DefaultUserAgent,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewForHost returns a client for a GitHub hostname, e.g. "github.com" or an
// Enterprise Server host.
func NewForHost(host, token string, opts ...Option) *Client {
	return New(token, append([]Option{WithBaseURL(APIBaseURL(host))}, opts...)...)
}

// APIBaseURL returns the REST API root for a GitHub host. GitHub.com serves
// its API from a separate hostname; GitHub Enterprise Server serves it from
// /api/v3 on the same host.
func APIBaseURL(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimRight(host, "/")
	if host == "" || host == "github.com" || host == "www.github.com" {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

// WebURL returns the browser-facing root for a GitHub host. The official
// runner's config.sh takes this URL, not the API one.
func WebURL(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimRight(host, "/")
	if host == "" {
		host = "github.com"
	}
	return "https://" + host
}

// RateLimit is the rate limit state reported by the most recent response.
type RateLimit struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
}

// do performs a request and decodes a JSON response into out. A nil out
// discards the body. It returns an *Error for any non-2xx response.
//
// It returns the response headers rather than the response: the body is always
// consumed and closed here, so handing back a *http.Response would invite
// callers to read from something already closed.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (http.Header, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	endpoint := c.baseURL.String() + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.Header, c.newError(resp)
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("decode %s %s response: %w", method, endpoint, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	}
	return resp.Header, nil
}

// authenticated reports whether a token was supplied.
func (c *Client) authenticated() bool { return c.token != "" }

func parseRateLimit(h http.Header) RateLimit {
	var rl RateLimit
	rl.Limit, _ = strconv.Atoi(h.Get("X-RateLimit-Limit"))
	rl.Remaining, _ = strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if sec, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil && sec > 0 {
		rl.Reset = time.Unix(sec, 0).UTC()
	}
	return rl
}
