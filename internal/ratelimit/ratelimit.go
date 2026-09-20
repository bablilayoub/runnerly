// Package ratelimit bounds how fast one client can hit the API.
//
// It is a token bucket per key, held in memory. That is deliberately modest:
// a single control plane is the normal deployment, and a shared limiter
// would mean Redis, which the plan says not to add without a concrete need.
// Behind several instances each enforces its own share, which is a weaker
// guarantee — stated here rather than discovered later.
package ratelimit

import (
	"sync"
	"time"
)

// Limit describes how many requests a key may make.
type Limit struct {
	// Burst is how many requests can arrive at once.
	Burst int
	// Every is how often one token is added back.
	Every time.Duration
}

// PerMinute builds a limit allowing n requests a minute, in bursts of n.
func PerMinute(n int) Limit {
	if n <= 0 {
		n = 1
	}
	return Limit{Burst: n, Every: time.Minute / time.Duration(n)}
}

// idleAfter is how long an unused bucket is kept before it is forgotten.
// Without this the map grows with every distinct client for ever.
const idleAfter = 10 * time.Minute

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter hands out tokens per key.
type Limiter struct {
	limit Limit
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
	// swept is when idle buckets were last cleared out.
	swept time.Time
}

// New returns a Limiter.
func New(limit Limit) *Limiter {
	if limit.Burst <= 0 {
		limit.Burst = 1
	}
	if limit.Every <= 0 {
		limit.Every = time.Second
	}
	return &Limiter{
		limit:   limit,
		now:     time.Now,
		buckets: map[string]*bucket{},
	}
}

// WithClock replaces the clock, so tests do not have to wait.
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.now = now
	return l
}

// Allow reports whether a request from key may proceed, and how long to wait
// if not.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok {
		// A new client starts with a full bucket minus this request, so the
		// first call is never refused.
		l.buckets[key] = &bucket{tokens: float64(l.limit.Burst) - 1, last: now}
		return true, 0
	}

	// Refill for the time that has passed, capped at the burst size.
	elapsed := now.Sub(b.last)
	b.last = now
	b.tokens += elapsed.Seconds() / l.limit.Every.Seconds()
	if b.tokens > float64(l.limit.Burst) {
		b.tokens = float64(l.limit.Burst)
	}

	if b.tokens < 1 {
		// How long until one whole token is available.
		missing := 1 - b.tokens
		return false, time.Duration(missing * float64(l.limit.Every))
	}
	b.tokens--
	return true, 0
}

// sweep forgets buckets nothing has used recently.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.swept) < idleAfter {
		return
	}
	l.swept = now
	for key, b := range l.buckets {
		if now.Sub(b.last) > idleAfter {
			delete(l.buckets, key)
		}
	}
}

// Len reports how many keys are being tracked, for tests and for a metric.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
