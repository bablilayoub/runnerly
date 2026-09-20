// Package controlplane is the agent's client for the Runnerly server.
//
// It deliberately does not import the server package. The agent binary should
// not carry a PostgreSQL driver just to describe a heartbeat, so the wire
// types are declared here and pinned to the server's by an integration test
// that runs a real server and talks to it with this client.
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnauthorized means the credential was refused. The agent treats it as
// terminal rather than retrying: a revoked token will not start working.
var ErrUnauthorized = errors.New("the control plane refused the credential")

// maxResponseBody bounds what the agent will read back.
const maxResponseBody = 1 << 20

// Client talks to a Runnerly control plane.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	userAgent  string
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
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

// New returns a client for a control plane. An empty token is valid for
// enrollment, where the enrollment token is passed to Register instead.
func New(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		userAgent:  "runnerly-agent",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Error is a failed response from the control plane.
type Error struct {
	StatusCode int
	Code       string `json:"error"`
	Message    string `json:"message"`
	Hint       string `json:"hint"`
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("the control plane returned %d", e.StatusCode)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Hint != "" {
		msg += "\n" + e.Hint
	}
	return msg
}

// RegisterRequest describes the machine enrolling.
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

// Runner is the subset of the server's runner the agent cares about.
type Runner struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Config is what the server tells the agent about how to behave.
type Config struct {
	HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds"`
}

// Interval returns the heartbeat interval, or a sensible default when the
// server did not say.
func (c Config) Interval() time.Duration {
	if c.HeartbeatIntervalSeconds <= 0 {
		return 20 * time.Second
	}
	return time.Duration(c.HeartbeatIntervalSeconds) * time.Second
}

// RegisterResponse carries the machine token, once.
type RegisterResponse struct {
	Runner       Runner `json:"runner"`
	MachineToken string `json:"machine_token"`
	Config       Config `json:"config"`
}

// Register enrolls the machine using a one-shot enrollment token.
func (c *Client) Register(ctx context.Context, enrollmentToken string, req RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/agent/register", enrollmentToken, req, &out); err != nil {
		return nil, err
	}
	if out.MachineToken == "" {
		return nil, errors.New("the control plane enrolled the machine but returned no machine token")
	}
	return &out, nil
}

// HeartbeatRequest is one report.
type HeartbeatRequest struct {
	Status        string  `json:"status"`
	StatusDetail  string  `json:"status_detail"`
	CPUPercent    float64 `json:"cpu"`
	MemoryPercent float64 `json:"memory"`
	DiskPercent   float64 `json:"disk"`
	RunnerVersion string  `json:"runner_version"`
	AgentVersion  string  `json:"agent_version"`
}

// PendingCommand is something an operator asked this agent to do.
type PendingCommand struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

// CommandRestart asks the agent to stop the runner and start it again.
//
// A command an agent does not recognize is reported back as failed rather
// than silently ignored, so an operator running a newer server against an
// older agent sees why nothing happened.
const CommandRestart = "restart"

// HeartbeatResponse is what the server believes after the report, plus
// anything it wants the agent to do.
type HeartbeatResponse struct {
	Runner   Runner           `json:"runner"`
	Config   Config           `json:"config"`
	Commands []PendingCommand `json:"commands,omitempty"`
}

// Heartbeat reports the agent's state.
func (c *Client) Heartbeat(ctx context.Context, req HeartbeatRequest) (*HeartbeatResponse, error) {
	var out HeartbeatResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/agent/heartbeat", c.token, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Event is one thing that happened.
type Event struct {
	Event    string         `json:"event"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	Data     map[string]any `json:"data,omitempty"`
}

type eventsRequest struct {
	Events []Event `json:"events"`
}

type eventsResponse struct {
	Stored int `json:"stored"`
}

// SendEvents reports a batch of events and returns how many were stored.
func (c *Client) SendEvents(ctx context.Context, events []Event) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	var out eventsResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/agent/events", c.token, eventsRequest{Events: events}, &out); err != nil {
		return 0, err
	}
	return out.Stored, nil
}

// CompleteCommand reports the outcome of a command. An empty failure means
// it succeeded.
func (c *Client) CompleteCommand(ctx context.Context, id, failure string) error {
	body := struct {
		Error string `json:"error"`
	}{Error: failure}
	return c.do(ctx, http.MethodPost, "/api/v1/agent/commands/"+url.PathEscape(id)+"/result",
		c.token, body, nil)
}

// Retire tells the control plane this runner has finished for good.
//
// An ephemeral runner calls it during teardown, so the dashboard can show a
// finished runner as finished rather than as one that stopped answering.
func (c *Client) Retire(ctx context.Context, reason string) error {
	body := struct {
		Reason string `json:"reason"`
	}{Reason: reason}
	return c.do(ctx, http.MethodPost, "/api/v1/agent/retire", c.token, body, nil)
}

// Health checks that the control plane is reachable. It needs no credential.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/v1/health", "", nil, nil)
}

func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	endpoint := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, payload)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.responseError(resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(out); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, endpoint, err)
	}
	return nil
}

func (c *Client) responseError(resp *http.Response) error {
	apiErr := &Error{StatusCode: resp.StatusCode}
	if body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody)); err == nil {
		_ = json.Unmarshal(body, apiErr)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: %s", ErrUnauthorized, apiErr.Error())
	}
	return apiErr
}
