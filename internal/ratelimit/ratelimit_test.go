package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// clock is a time source a test drives by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Unix(1700000000, 0)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestBurstIsAllowedThenRefused(t *testing.T) {
	c := newClock()
	l := New(Limit{Burst: 3, Every: time.Second}).WithClock(c.Now)

	for i := 1; i <= 3; i++ {
		if ok, _ := l.Allow("client"); !ok {
			t.Fatalf("request %d was refused inside the burst", i)
		}
	}

	ok, retry := l.Allow("client")
	if ok {
		t.Fatal("a fourth request was allowed past the burst")
	}
	if retry <= 0 || retry > time.Second {
		t.Errorf("retry after = %v, want something under a second", retry)
	}
}

func TestTokensComeBackOverTime(t *testing.T) {
	c := newClock()
	l := New(Limit{Burst: 2, Every: time.Second}).WithClock(c.Now)

	l.Allow("client")
	l.Allow("client")
	if ok, _ := l.Allow("client"); ok {
		t.Fatal("the bucket was not empty")
	}

	c.advance(time.Second)
	if ok, _ := l.Allow("client"); !ok {
		t.Error("a token did not come back after the refill interval")
	}
	// Only one came back, not the whole burst.
	if ok, _ := l.Allow("client"); ok {
		t.Error("more tokens came back than time had passed for")
	}
}

func TestRefillIsCappedAtTheBurst(t *testing.T) {
	c := newClock()
	l := New(Limit{Burst: 2, Every: time.Second}).WithClock(c.Now)

	l.Allow("client")
	// An idle client must not bank credit for a huge burst later.
	c.advance(time.Hour)

	if ok, _ := l.Allow("client"); !ok {
		t.Fatal("the first request after idling was refused")
	}
	if ok, _ := l.Allow("client"); !ok {
		t.Fatal("the second request after idling was refused")
	}
	if ok, _ := l.Allow("client"); ok {
		t.Error("an idle client banked more than the burst")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	c := newClock()
	l := New(Limit{Burst: 1, Every: time.Minute}).WithClock(c.Now)

	if ok, _ := l.Allow("first"); !ok {
		t.Fatal("the first client was refused")
	}
	if ok, _ := l.Allow("second"); !ok {
		t.Error("one client's usage limited another")
	}
	if ok, _ := l.Allow("first"); ok {
		t.Error("the first client was not limited")
	}
}

func TestIdleKeysAreForgotten(t *testing.T) {
	c := newClock()
	l := New(Limit{Burst: 1, Every: time.Second}).WithClock(c.Now)

	for i := range 50 {
		l.Allow(string(rune('a' + i%26)))
	}
	if l.Len() == 0 {
		t.Fatal("nothing was tracked")
	}

	// Long enough that every bucket is idle, then one more request to
	// trigger the sweep.
	c.advance(2 * idleAfter)
	l.Allow("fresh")

	if l.Len() != 1 {
		t.Errorf("tracking %d keys after the sweep, want only the fresh one", l.Len())
	}
}

func TestPerMinute(t *testing.T) {
	l := PerMinute(60)
	if l.Burst != 60 {
		t.Errorf("Burst = %d, want 60", l.Burst)
	}
	if l.Every != time.Second {
		t.Errorf("Every = %v, want a second", l.Every)
	}
	// A nonsensical rate still produces a usable limit rather than a
	// division by zero.
	if got := PerMinute(0); got.Burst != 1 {
		t.Errorf("PerMinute(0) = %+v", got)
	}
}

func TestZeroValuesAreCorrected(t *testing.T) {
	l := New(Limit{})
	if ok, _ := l.Allow("client"); !ok {
		t.Error("a zero limit refused the first request")
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	l := New(Limit{Burst: 1000, Every: time.Millisecond})

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for range 20 {
				l.Allow(string(rune('a' + n%26)))
			}
		}(i)
	}
	wg.Wait()

	if l.Len() == 0 {
		t.Error("nothing was tracked")
	}
}
