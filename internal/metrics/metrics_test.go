package metrics

import (
	"strings"
	"sync"
	"testing"
)

func render(t *testing.T, r *Registry) string {
	t.Helper()
	var b strings.Builder
	if err := r.Write(&b); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return b.String()
}

func TestCounterWithoutLabels(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("runnerly_heartbeats_total", "Heartbeats received.", "")

	// An untouched counter still appears, so a dashboard shows zero rather
	// than "no data".
	out := render(t, r)
	if !strings.Contains(out, "runnerly_heartbeats_total 0") {
		t.Errorf("an unincremented counter is missing:\n%s", out)
	}

	c.Inc("")
	c.Add("", 4)
	out = render(t, r)
	if !strings.Contains(out, "runnerly_heartbeats_total 5") {
		t.Errorf("counter = wrong value:\n%s", out)
	}
	if c.Value("") != 5 {
		t.Errorf("Value() = %d, want 5", c.Value(""))
	}
}

func TestCounterWithLabels(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("runnerly_requests_total", "Requests handled.", "code")

	c.Inc("200")
	c.Inc("200")
	c.Inc("404")

	out := render(t, r)
	if !strings.Contains(out, `runnerly_requests_total{code="200"} 2`) {
		t.Errorf("labeled counter wrong:\n%s", out)
	}
	if !strings.Contains(out, `runnerly_requests_total{code="404"} 1`) {
		t.Errorf("labeled counter wrong:\n%s", out)
	}
}

func TestOutputIsStable(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("runnerly_requests_total", "Requests handled.", "code")
	for _, code := range []string{"500", "200", "404", "201"} {
		c.Inc(code)
	}

	// Two scrapes must be byte-identical, so diffing them shows real
	// changes rather than map ordering.
	if first, second := render(t, r), render(t, r); first != second {
		t.Errorf("scrapes differ:\n%s\n---\n%s", first, second)
	}

	out := render(t, r)
	order := []string{`code="200"`, `code="201"`, `code="404"`, `code="500"`}
	last := -1
	for _, want := range order {
		at := strings.Index(out, want)
		if at < 0 {
			t.Fatalf("missing %s:\n%s", want, out)
		}
		if at < last {
			t.Errorf("labels are not sorted:\n%s", out)
		}
		last = at
	}
}

func TestGaugeIsReadAtScrapeTime(t *testing.T) {
	r := NewRegistry()
	value := 3.0
	// A gauge read at scrape time cannot go stale because someone forgot
	// to update it.
	r.NewGauge("runnerly_runners_online", "Runners online.", func() float64 { return value })

	if out := render(t, r); !strings.Contains(out, "runnerly_runners_online 3") {
		t.Errorf("gauge = wrong:\n%s", out)
	}
	value = 7
	if out := render(t, r); !strings.Contains(out, "runnerly_runners_online 7") {
		t.Errorf("gauge did not follow the value:\n%s", out)
	}
}

func TestExpositionFormat(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("runnerly_things_total", "How many things.", "").Inc("")
	r.NewGauge("runnerly_now", "A gauge.", func() float64 { return 1.5 })

	out := render(t, r)
	for _, want := range []string{
		"# HELP runnerly_things_total How many things.",
		"# TYPE runnerly_things_total counter",
		"# HELP runnerly_now A gauge.",
		"# TYPE runnerly_now gauge",
		"runnerly_now 1.5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Every line must be a comment or a sample; a stray blank line makes
	// some scrapers unhappy.
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			t.Errorf("blank line in the exposition:\n%s", out)
		}
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("runnerly_odd_total", "Odd labels.", "name")
	c.Inc(`a "quoted" \ value`)
	c.Inc("with\nnewline")

	out := render(t, r)
	if !strings.Contains(out, `name="a \"quoted\" \\ value"`) {
		t.Errorf("quotes and backslashes were not escaped:\n%s", out)
	}
	if strings.Contains(out, "with\nnewline") {
		t.Errorf("a newline leaked into a label, breaking the format:\n%s", out)
	}
	if !strings.Contains(out, `name="with\nnewline"`) {
		t.Errorf("newline was not escaped:\n%s", out)
	}
}

func TestWholeNumbersHaveNoDecimalPoint(t *testing.T) {
	r := NewRegistry()
	r.NewGauge("runnerly_count", "A count.", func() float64 { return 42 })
	if out := render(t, r); !strings.Contains(out, "runnerly_count 42\n") {
		t.Errorf("gauge = %q, want a plain integer", out)
	}
}

func TestConcurrentCountersAreSafe(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("runnerly_concurrent_total", "Concurrency.", "worker")

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			label := string(rune('a' + n%5))
			for range 100 {
				c.Inc(label)
			}
		}(i)
	}
	wg.Wait()

	var total int64
	for _, label := range []string{"a", "b", "c", "d", "e"} {
		total += c.Value(label)
	}
	if total != 2000 {
		t.Errorf("total = %d, want 2000", total)
	}
}
