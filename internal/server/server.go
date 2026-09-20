// Package server is the Runnerly control plane's HTTP API.
//
// It does not schedule anything. GitHub assigns jobs to runners; this server
// keeps track of the machines around them: which exist, whether they are
// alive, what has happened to them, and who is allowed to ask.
package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bablilayoub/runnerly/internal/secret"
	"github.com/bablilayoub/runnerly/internal/store"
	"github.com/bablilayoub/runnerly/internal/version"
	"github.com/bablilayoub/runnerly/internal/web"
)

// Component names this server in logs.
const Component = "control-plane"

// OAuthConfig is the GitHub OAuth App the dashboard signs in through.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	// AllowedLogins restricts who may sign in. Empty allows anyone who
	// completes the flow.
	AllowedLogins []string
	// GitHubHost is the GitHub deployment to authenticate against.
	GitHubHost string
}

// Options configures a Server.
type Options struct {
	Store  *store.Store
	Logger *slog.Logger
	// Key encrypts user credentials at rest. Nil means user tokens are not
	// stored at all, which is a valid way to run the server.
	Key   *secret.Key
	OAuth OAuthConfig
	// PublicURL is where the server is reachable, used to build the OAuth
	// callback. Empty disables sign-in.
	PublicURL string
	// Thresholds decide when a runner counts as stale or offline.
	Thresholds store.Thresholds
	// HeartbeatInterval is what the server tells agents to use.
	HeartbeatInterval time.Duration
	// HTTPClient is used for GitHub calls during sign-in.
	HTTPClient *http.Client
	// Now is the clock, injectable so tests can age a heartbeat without
	// waiting.
	Now func() time.Time
}

// Server serves the control plane API.
type Server struct {
	store             *store.Store
	logger            *slog.Logger
	key               *secret.Key
	oauth             OAuthConfig
	publicURL         string
	thresholds        store.Thresholds
	heartbeatInterval time.Duration
	httpClient        *http.Client
	now               func() time.Time
	secureCookies     bool
	handler           http.Handler
}

// New builds a Server.
func New(opts Options) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("server: no store")
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if opts.Thresholds.Stale == 0 && opts.Thresholds.Offline == 0 {
		opts.Thresholds = store.DefaultThresholds()
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 20 * time.Second
	}
	if opts.OAuth.GitHubHost == "" {
		opts.OAuth.GitHubHost = "github.com"
	}

	s := &Server{
		store:             opts.Store,
		logger:            opts.Logger,
		key:               opts.Key,
		oauth:             opts.OAuth,
		publicURL:         strings.TrimRight(opts.PublicURL, "/"),
		thresholds:        opts.Thresholds,
		heartbeatInterval: opts.HeartbeatInterval,
		httpClient:        opts.HTTPClient,
		now:               opts.Now,
		// Cookies are only marked Secure when the server is actually served
		// over HTTPS; setting it on a plain-HTTP dev server would silently
		// stop sign-in working with no clue why.
		secureCookies: strings.HasPrefix(opts.PublicURL, "https://"),
	}
	s.handler = s.routes()

	// Session cookies over plain HTTP to anything but loopback are readable
	// by anyone on the path. Runnerly still serves, because a reverse proxy
	// terminating TLS is a normal deployment and the server cannot see it —
	// but it says so once, loudly, rather than staying quiet about it.
	if opts.PublicURL != "" && !s.secureCookies && !isLoopbackURL(opts.PublicURL) {
		s.logger.Warn("server.url is not https, so session cookies will be sent without the Secure flag",
			"component", Component,
			"event", "insecure_cookies",
			"url", opts.PublicURL,
			"hint", "terminate TLS in front of the server and set server.url to its https address",
		)
	}
	return s, nil
}

// isLoopbackURL reports whether a URL points at this machine, where plain
// HTTP is a reasonable way to run a development server.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// Handler returns the HTTP handler, for tests and for embedding.
func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// routes builds the mux.
//
// Go's own ServeMux handles method and pattern matching, so Runnerly needs no
// routing dependency.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Unauthenticated: a health check that needs a credential is useless to
	// a load balancer.
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	// Agents. Enrollment carries an enrollment token; everything after it
	// carries the machine token enrollment returned.
	mux.HandleFunc("POST /api/v1/agent/register", s.handleAgentRegister)
	mux.HandleFunc("POST /api/v1/agent/heartbeat", s.requireMachine(s.handleAgentHeartbeat))
	mux.HandleFunc("POST /api/v1/agent/events", s.requireMachine(s.handleAgentEvents))
	mux.HandleFunc("GET /api/v1/agent/config", s.requireMachine(s.handleAgentConfig))
	mux.HandleFunc("POST /api/v1/agent/commands/{id}/result", s.requireMachine(s.handleAgentCommandResult))

	// Dashboard sign-in.
	mux.HandleFunc("GET /api/v1/auth/config", s.handleAuthConfig)
	mux.HandleFunc("GET /api/v1/auth/github", s.handleAuthStart)
	mux.HandleFunc("GET /api/v1/auth/github/callback", s.handleAuthCallback)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("GET /api/v1/auth/session", s.requireUser(s.handleAuthSession))

	// Dashboard data.
	mux.HandleFunc("GET /api/v1/overview", s.requireUser(s.handleOverview))
	mux.HandleFunc("GET /api/v1/runners", s.requireUser(s.handleListRunners))
	mux.HandleFunc("GET /api/v1/runners/{id}", s.requireUser(s.handleGetRunner))
	mux.HandleFunc("DELETE /api/v1/runners/{id}", s.requireUser(s.handleDeleteRunner))
	mux.HandleFunc("POST /api/v1/runners/{id}/restart", s.requireUser(s.handleRestartRunner))
	mux.HandleFunc("GET /api/v1/runners/{id}/commands", s.requireUser(s.handleListCommands))
	mux.HandleFunc("GET /api/v1/events", s.requireUser(s.handleListEvents))

	// Enrollment tokens are operator tools, so they need a signed-in user.
	mux.HandleFunc("GET /api/v1/enrollment-tokens", s.requireUser(s.handleListEnrollmentTokens))
	mux.HandleFunc("POST /api/v1/enrollment-tokens", s.requireUser(s.handleCreateEnrollmentToken))
	mux.HandleFunc("DELETE /api/v1/enrollment-tokens/{id}", s.requireUser(s.handleRevokeEnrollmentToken))

	// An unknown path under /api is an API error, not a page. Anything else
	// belongs to the dashboard's own router.
	mux.HandleFunc("/api/", s.handleNotFound)
	mux.Handle("/", web.Handler())

	return s.withRecovery(s.withRequestLog(mux))
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, http.StatusNotFound, "no_such_endpoint",
		r.Method+" "+r.URL.Path+" is not an endpoint on this server.",
		"The API is under /api/v1. See docs/server.md.")
}

// HealthResponse is what /health returns.
type HealthResponse struct {
	Status   string `json:"status"`
	Version  string `json:"version"`
	Database string `json:"database"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	body := HealthResponse{Status: "ok", Version: version.Get().Short(), Database: "ok"}
	status := http.StatusOK

	if err := s.store.Ping(ctx); err != nil {
		// The server process is up but cannot do its job, so this is not a
		// 200. A load balancer should take it out of rotation.
		body.Status = "degraded"
		body.Database = "unreachable"
		status = http.StatusServiceUnavailable
		s.log(r).Error("the database is unreachable", "event", "health_degraded", "error", err.Error())
	}
	s.writeJSON(w, r, status, body)
}

// withRequestLog records one line per request.
func (s *Server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := s.now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		s.logger.Info("request",
			"component", Component,
			"event", "http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", s.now().Sub(started).Milliseconds(),
		)
	})
}

// withRecovery turns a panic into a 500 instead of a dropped connection.
func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("a handler panicked",
					"component", Component,
					"event", "panic",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", recovered,
				)
				s.fail(w, r, http.StatusInternalServerError, "internal_error",
					"Something went wrong on the server.", "Check the server logs for the detail.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder remembers the status code for the request log.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.written {
		r.status = status
		r.written = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}
