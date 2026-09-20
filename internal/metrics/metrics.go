// Package metrics exposes Prometheus metrics for the control plane.
//
// The exposition format is written by hand rather than pulled in with the
// client library. The plan asks for a handful of counters and gauges and
// says not to build an observability stack; the text format is a few lines
// per metric, and the library would be the largest dependency in the
// project by some margin.
//
// What that gives up is real and worth naming: no histograms, no exemplars,
// no native histogram support, and no protection against a metric name
// being misspelled. Registry.Write is the one place that could go wrong, so
// it is the one place that is tested hard.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Type is a Prometheus metric type.
type Type string

const (
	// Counter only ever goes up.
	Counter Type = "counter"
	// Gauge can go up or down.
	Gauge Type = "gauge"
)

// CounterVec is a monotonically increasing value, optionally split by one
// label.
type CounterVec struct {
	name string
	help string

	mu     sync.RWMutex
	values map[string]*atomic.Int64
	// labelName is empty for an unlabeled counter.
	labelName string
}

// Registry holds the metrics a process publishes.
type Registry struct {
	mu       sync.RWMutex
	counters []*CounterVec
	gauges   []*gauge
}

type gauge struct {
	name string
	help string
	// read supplies the current value when the registry is scraped, so a
	// gauge is never stale and nothing has to remember to update it.
	read func() float64
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// NewCounter registers a counter. labelName may be empty for one without
// labels.
func (r *Registry) NewCounter(name, help, labelName string) *CounterVec {
	c := &CounterVec{
		name:      name,
		help:      help,
		labelName: labelName,
		values:    map[string]*atomic.Int64{},
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = append(r.counters, c)
	return c
}

// NewGauge registers a gauge whose value is read at scrape time.
func (r *Registry) NewGauge(name, help string, read func() float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges = append(r.gauges, &gauge{name: name, help: help, read: read})
}

// Inc adds one to the counter for a label value. Pass "" when the counter
// has no label.
func (c *CounterVec) Inc(label string) { c.Add(label, 1) }

// Add increases the counter for a label value.
func (c *CounterVec) Add(label string, delta int64) {
	c.mu.RLock()
	v, ok := c.values[label]
	c.mu.RUnlock()
	if ok {
		v.Add(delta)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Another goroutine may have created it between the two locks.
	if v, ok := c.values[label]; ok {
		v.Add(delta)
		return
	}
	var fresh atomic.Int64
	fresh.Add(delta)
	c.values[label] = &fresh
}

// Value returns the current count for a label, for tests.
func (c *CounterVec) Value(label string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.values[label]; ok {
		return v.Load()
	}
	return 0
}

// Write renders the registry in Prometheus' text exposition format.
func (r *Registry) Write(w io.Writer) error {
	r.mu.RLock()
	counters := append([]*CounterVec(nil), r.counters...)
	gauges := append([]*gauge(nil), r.gauges...)
	r.mu.RUnlock()

	var b strings.Builder
	for _, c := range counters {
		writeHeader(&b, c.name, c.help, Counter)

		c.mu.RLock()
		labels := make([]string, 0, len(c.values))
		for label := range c.values {
			labels = append(labels, label)
		}
		// Sorted, so a scrape is byte-identical between calls and a diff of
		// two scrapes shows real changes.
		sort.Strings(labels)

		if len(labels) == 0 && c.labelName == "" {
			// An unlabeled counter that has never been incremented should
			// still appear, or a dashboard shows "no data" rather than zero.
			fmt.Fprintf(&b, "%s 0\n", c.name)
		}
		for _, label := range labels {
			value := c.values[label].Load()
			if c.labelName == "" {
				fmt.Fprintf(&b, "%s %d\n", c.name, value)
				continue
			}
			// The quotes are written here rather than with %q, which would
			// escape a second time on top of escapeLabel.
			fmt.Fprintf(&b, "%s{%s=\"%s\"} %d\n", c.name, c.labelName, escapeLabel(label), value)
		}
		c.mu.RUnlock()
	}

	for _, g := range gauges {
		writeHeader(&b, g.name, g.help, Gauge)
		fmt.Fprintf(&b, "%s %s\n", g.name, formatFloat(g.read()))
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func writeHeader(b *strings.Builder, name, help string, t Type) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, strings.ReplaceAll(help, "\n", " "))
	fmt.Fprintf(b, "# TYPE %s %s\n", name, t)
}

// escapeLabel escapes what a label value may not contain.
func escapeLabel(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

// formatFloat renders a value the way Prometheus expects, without a
// trailing ".0" on whole numbers.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// ContentType is what a scrape response should be served as.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"
