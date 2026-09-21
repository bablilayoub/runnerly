//go:build linux

package machine

import (
	"context"
	"os"
	"strconv"
	"strings"
)

// MemoryTotal returns physical memory in bytes, or 0 if it cannot be read.
//
// Inside a container with a memory limit the honest answer is the limit,
// not the host's RAM: a runner capped at 2 GiB is a 2 GiB machine as far as
// anything it runs is concerned, and reporting the host's 64 GiB would put
// a number on the dashboard that no workload can reach.
func MemoryTotal() int64 {
	total, _ := meminfo("MemTotal:")
	if limit, ok := cgroupMemoryLimit(); ok && (total == 0 || limit < total) {
		return limit
	}
	return total
}

// memoryUsedPercent reports how much memory is committed.
//
// The measure is MemAvailable, which is the kernel's own estimate of what a
// new workload could get without swapping. It is not MemFree: on any
// machine that has been up a while almost nothing is free, because the
// kernel spends the rest on page cache it will hand back on demand.
// Reporting MemFree would show every healthy build machine at 97% used.
func memoryUsedPercent(context.Context) (float64, bool) {
	if used, total, ok := cgroupMemoryUsage(); ok {
		return percentUsed(float64(used), float64(total))
	}

	total, ok := meminfo("MemTotal:")
	if !ok || total == 0 {
		return 0, false
	}
	available, ok := meminfo("MemAvailable:")
	if !ok {
		// Every kernel since 3.14 publishes it. Older than that, or a
		// /proc that has been filtered, gets a dash rather than a figure
		// derived from fields that mean something else.
		return 0, false
	}
	return percentUsed(float64(total-available), float64(total))
}

// meminfo reads one "Key: 1234 kB" line out of /proc/meminfo, in bytes.
func meminfo(key string) (int64, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	return parseMeminfo(string(data), key)
}

// parseMeminfo is the part of meminfo that does not touch the filesystem,
// separated so the format can be tested without one.
func parseMeminfo(data, key string) (int64, bool) {
	for line := range strings.Lines(data) {
		rest, found := strings.CutPrefix(line, key)
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 1 {
			return 0, false
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

// cgroupRoot is where a cgroup v2 hierarchy is mounted. A process's own
// limits appear at the root of its namespace, which is what a container
// sees.
//
// Only v2 is read. v1 keeps the same numbers under different names, but
// none of this has been run against a v1 host, and a number arrived at by
// reading documentation is exactly the kind that turns out to mean
// something else.
const cgroupRoot = "/sys/fs/cgroup"

// cgroupMemoryLimit returns this cgroup's memory ceiling.
//
// "max" means no limit, which is the usual answer outside a container.
func cgroupMemoryLimit() (int64, bool) {
	return cgroupValue(cgroupRoot + "/memory.max")
}

// cgroupMemoryUsage returns used and total bytes inside a limited cgroup.
//
// memory.current counts page cache, which would overstate use the same way
// MemFree does, so the reclaimable part is subtracted using the file and
// slab-reclaimable figures the kernel breaks out in memory.stat.
func cgroupMemoryUsage() (used, total int64, ok bool) {
	total, ok = cgroupMemoryLimit()
	if !ok || total <= 0 {
		return 0, 0, false
	}
	current, ok := cgroupValue(cgroupRoot + "/memory.current")
	if !ok {
		return 0, 0, false
	}

	reclaimable := cgroupStat("inactive_file") + cgroupStat("slab_reclaimable")
	used = current - reclaimable
	if used < 0 {
		used = 0
	}
	return used, total, true
}

// cgroupValue reads a file holding a single number. "max" is not a number.
func cgroupValue(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// cgroupStat reads one key out of memory.stat, or 0 if it is not there.
func cgroupStat(key string) int64 {
	data, err := os.ReadFile(cgroupRoot + "/memory.stat")
	if err != nil {
		return 0
	}
	for line := range strings.Lines(string(data)) {
		name, value, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found || name != key {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// newCPUMeter returns a meter that differences /proc/stat.
func newCPUMeter() cpuMeter {
	m := &procStatMeter{}
	// Prime it, so the first real reading has something to subtract. The
	// result is discarded: there is nothing to compare against yet.
	m.percent(context.Background())
	return m
}

// procStatMeter measures utilization by differencing the kernel's tick
// counters. The window is the time between calls.
//
// Inside a container this still reports the host: /proc/stat is not
// namespaced, and cgroup v2's cpu.stat would have to be weighed against a
// quota to mean anything. A containerized agent's processor figure is
// therefore its host's, which is written down here rather than discovered.
type procStatMeter struct {
	prevBusy, prevTotal int64
	primed              bool
}

func (m *procStatMeter) percent(context.Context) (float64, bool) {
	busy, total, ok := procStat()
	if !ok {
		return 0, false
	}
	defer func() {
		m.prevBusy, m.prevTotal, m.primed = busy, total, true
	}()

	if !m.primed {
		return 0, false
	}
	deltaTotal := total - m.prevTotal
	if deltaTotal <= 0 {
		// No time passed, or the counters were reset under us.
		return 0, false
	}
	return percentUsed(float64(busy-m.prevBusy), float64(deltaTotal))
}

// procStat sums the aggregate "cpu" line into busy and total ticks.
func procStat() (busy, total int64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	return parseProcStat(string(data))
}

// parseProcStat reads the aggregate "cpu" line, which is the first.
func parseProcStat(data string) (busy, total int64, ok bool) {
	line, _, _ := strings.Cut(data, "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}

	// user nice system idle iowait irq softirq steal, and then guest and
	// guest_nice, which are already counted inside user and nice. Stopping
	// at steal is what avoids counting a guest's time twice.
	const lastCounted = 8
	for i, field := range fields[1:] {
		if i >= lastCounted {
			break
		}
		ticks, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += ticks
		// idle and iowait, the two the machine was not working during.
		if i != 3 && i != 4 {
			busy += ticks
		}
	}
	return busy, total, true
}
