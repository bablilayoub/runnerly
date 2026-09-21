// Package machine reads what this computer is and how hard it is working.
//
// Two kinds of fact live here. Totals — memory, disk — describe the machine
// and are read once, at enrollment. Load — processor, memory and disk
// utilization — describes the moment and is read over and over.
//
// There is no portable source for any of it, so every reading has a
// per-platform implementation and a documented definition. A platform that
// cannot answer reports zero, which every surface renders as a dash: a
// machine that will not say is shown as unknown rather than as idle.
package machine

import (
	"context"
	"sync"
	"time"
)

// Load is how hard a machine is working, as percentages from 0 to 100.
//
// Each field is zero when that reading could not be taken.
type Load struct {
	// CPUPercent is processor utilization: time spent doing anything other
	// than idling, over all cores.
	CPUPercent float64
	// MemoryPercent is how much memory is committed, measured against what
	// a new workload could actually get rather than against what is
	// nominally free. Page cache the kernel would drop on demand does not
	// count as used.
	MemoryPercent float64
	// DiskPercent is how full the filesystem holding the runner is, using
	// the same definition df does: space reserved for root counts as used
	// rather than as headroom nobody can reach.
	DiskPercent float64
}

// Sampler takes readings in the background so a caller never waits for one.
//
// This matters more than it looks: on Linux processor utilization is a
// difference between two counters and costs a file read, but on macOS there
// is no counter to difference and the only honest answer comes from a
// one-second sample taken by a subprocess. Blocking a heartbeat for a second
// to decorate it is the wrong trade, so heartbeats read whatever the last
// refresh produced.
type Sampler struct {
	path string
	cpu  cpuMeter

	mu   sync.Mutex
	last Load
}

// NewSampler returns a sampler for the filesystem holding path.
//
// Constructing one primes the processor meter, so the first refresh has a
// previous reading to difference against.
func NewSampler(path string) *Sampler {
	return &Sampler{path: path, cpu: newCPUMeter()}
}

// warmup is how long Run waits before its first reading.
//
// Long enough that the processor delta covers a meaningful window, short
// enough that the first heartbeat carries a number. Without it a fresh
// machine shows dashes until its second heartbeat, which reads as broken.
const warmup = 3 * time.Second

// defaultInterval matches the agent's default heartbeat.
const defaultInterval = 20 * time.Second

// Run refreshes the reading every interval until ctx ends.
//
// On Linux the interval is also the processor measurement window, which is
// deliberate: an average over the time between heartbeats describes a
// machine better than a spot reading taken at the instant one was due.
func (s *Sampler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultInterval
	}
	first := warmup
	if interval < first {
		first = interval
	}

	timer := time.NewTimer(first)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.refresh(ctx)
			timer.Reset(interval)
		}
	}
}

// Latest returns the most recent reading without taking a new one.
//
// The zero Load, meaning every reading unknown, is what a caller gets
// before the first refresh.
func (s *Sampler) Latest() Load {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// refresh takes a reading and replaces the stored one.
//
// A reading that fails is stored as zero rather than leaving the previous
// value in place. Stale numbers presented as current are worse than a dash,
// because nothing on the page says how old they are.
func (s *Sampler) refresh(ctx context.Context) {
	load := s.sample(ctx)

	s.mu.Lock()
	s.last = load
	s.mu.Unlock()
}

// sample takes one reading. It blocks, and on macOS for about a second.
func (s *Sampler) sample(ctx context.Context) Load {
	var load Load
	if cpu, ok := s.cpu.percent(ctx); ok {
		load.CPUPercent = clamp(cpu)
	}
	if mem, ok := memoryUsedPercent(ctx); ok {
		load.MemoryPercent = clamp(mem)
	}
	if disk, ok := diskUsedPercent(s.path); ok {
		load.DiskPercent = clamp(disk)
	}
	return load
}

// cpuMeter measures processor utilization.
//
// The window is up to the implementation: on Linux it is the time since the
// previous call, on macOS a one-second sample. Both are honest answers to
// "how busy is this machine", and neither can be had the other's way.
type cpuMeter interface {
	percent(ctx context.Context) (float64, bool)
}

// clamp keeps a percentage inside 0..100.
//
// Counters can go backwards across a suspend or a core coming online, and a
// dashboard reading 4000% is a bug report rather than information.
func clamp(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}

// percentUsed turns a used/total pair into a percentage, reporting whether
// the pair made sense at all.
func percentUsed(used, total float64) (float64, bool) {
	if total <= 0 || used < 0 {
		return 0, false
	}
	return used / total * 100, true
}
