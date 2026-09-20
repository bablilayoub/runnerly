package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bablilayoub/runnerly/internal/metrics"
	"github.com/bablilayoub/runnerly/internal/ratelimit"
	"github.com/bablilayoub/runnerly/internal/store"
)

// fleetSnapshot is the counts the gauges report.
//
// They are refreshed on a timer rather than read from the database on every
// scrape: a gauge callback cannot take a context or fail, and a scrape every
// few seconds should not put load on the database or block on it.
type fleetSnapshot struct {
	counts atomic.Pointer[store.Counts]
}

func (f *fleetSnapshot) get() store.Counts {
	if c := f.counts.Load(); c != nil {
		return *c
	}
	return store.Counts{}
}

// metricsRefresh is how often the fleet gauges are recomputed.
const metricsRefresh = 15 * time.Second

// registerMetrics wires up the metrics the plan asks for.
func (s *Server) registerMetrics() {
	r := metrics.NewRegistry()

	s.heartbeats = r.NewCounter("runnerly_runner_heartbeats_total",
		"Heartbeats accepted from agents.", "")
	s.restarts = r.NewCounter("runnerly_runner_restarts_total",
		"Runner restarts reported by agents.", "")
	s.agentErrors = r.NewCounter("runnerly_agent_errors_total",
		"Error-severity events reported by agents.", "")
	s.requests = r.NewCounter("runnerly_http_requests_total",
		"HTTP requests handled, by status class.", "status")
	s.rateLimited = r.NewCounter("runnerly_rate_limited_total",
		"Requests refused for exceeding a rate limit.", "")

	r.NewGauge("runnerly_runners_total",
		"Runners in service, excluding retired ones.",
		func() float64 { return float64(s.fleet.get().Total) })
	r.NewGauge("runnerly_runners_online",
		"Runners currently online or starting.",
		func() float64 { return float64(s.fleet.get().Online) })
	r.NewGauge("runnerly_runners_busy",
		"Runners currently running a job.",
		func() float64 { return float64(s.fleet.get().Busy) })
	r.NewGauge("runnerly_runners_offline",
		"Runners that have stopped reporting.",
		func() float64 { return float64(s.fleet.get().Offline) })
	r.NewGauge("runnerly_runners_retired",
		"Runners that finished for good, which is how an ephemeral run ends.",
		func() float64 { return float64(s.fleet.get().Retired) })

	s.metrics = r
}

// RefreshMetrics recomputes the fleet gauges. The server calls it on a
// timer; it is exported so a caller can drive it in a test.
func (s *Server) RefreshMetrics(ctx context.Context) {
	counts, err := s.store.CountRunners(ctx, s.now(), s.thresholds)
	if err != nil {
		s.logger.Warn("could not refresh the fleet metrics",
			"component", Component, "event", "metrics_refresh_failed", "error", err.Error())
		return
	}
	s.fleet.counts.Store(&counts)
}

// RunBackground keeps the metrics fresh until the context ends.
func (s *Server) RunBackground(ctx context.Context) {
	s.RefreshMetrics(ctx)

	ticker := time.NewTicker(metricsRefresh)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RefreshMetrics(ctx)
		}
	}
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.metricsEnabled {
		s.fail(w, r, http.StatusNotFound, "metrics_disabled",
			"Metrics are not enabled on this server.",
			"Set server.metrics.enabled to true.")
		return
	}
	if s.metricsToken != "" && !store.SecureCompare(bearerToken(r), s.metricsToken) {
		s.fail(w, r, http.StatusUnauthorized, "invalid_credentials",
			"Scraping this server needs the metrics token.",
			"Send it as `Authorization: Bearer <server.metrics.token>`.")
		return
	}

	w.Header().Set("Content-Type", metrics.ContentType)
	if err := s.metrics.Write(w); err != nil {
		s.log(r).Error("could not write the metrics", "event", "metrics_failed", "error", err.Error())
	}
}

// clientKey identifies the caller for rate limiting.
//
// X-Forwarded-For is only believed when configured, because anyone can send
// it: trusting it by default would let a client pick its own bucket and
// defeat the limit entirely.
func (s *Server) clientKey(r *http.Request) string {
	if s.trustForwardedFor {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			if first, _, found := strings.Cut(forwarded, ","); found {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(forwarded)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// withRateLimit refuses a caller that is going too fast.
func (s *Server) withRateLimit(limiter *ratelimit.Limiter, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, retry := limiter.Allow(s.clientKey(r))
		if allowed {
			next.ServeHTTP(w, r)
			return
		}

		s.rateLimited.Inc("")
		// Retry-After is in seconds and must be at least one, or a client
		// reads it as "now" and hammers straight back.
		seconds := int(retry.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		s.fail(w, r, http.StatusTooManyRequests, "rate_limited",
			"Too many requests from this address.",
			"Wait "+strconv.Itoa(seconds)+"s and try again.")
	})
}

// statusClass buckets a status code, so the metric has a handful of label
// values rather than one per code.
func statusClass(code int) string {
	switch {
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}
