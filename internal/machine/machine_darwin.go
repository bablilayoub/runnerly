//go:build darwin

package machine

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MemoryTotal asks the kernel for physical memory.
//
// hw.memsize is a 64-bit value and the standard library only exposes
// syscall.SysctlUint32, which would silently truncate any machine with
// more than 4 GiB — that is, all of them. syscall.Sysctl returns a string
// built from the raw bytes with a trailing NUL trimmed, which mangles
// binary values.
//
// So this reads the documented CLI instead. It runs once, at enrollment,
// and a machine that will not answer reports 0, which every surface shows
// as unknown.
func MemoryTotal() int64 {
	out, ok := run(context.Background(), 2*time.Second, "sysctl", "-n", "hw.memsize")
	if !ok {
		return 0
	}
	size, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0
	}
	return size
}

// memoryUsedPercent reports how much memory is committed, using the figure
// Activity Monitor calls Memory Used:
//
//	(anonymous - purgeable) + wired + occupied by compressor
//
// That is the one an operator can cross-check on the machine itself. It
// deliberately excludes file-backed pages, which the kernel drops on
// demand, and deliberately includes compressed pages, which still hold
// something: macOS compresses where Linux would swap, so leaving the
// compressor out would show a machine under real pressure as comfortable.
func memoryUsedPercent(ctx context.Context) (float64, bool) {
	total := MemoryTotal()
	if total <= 0 {
		return 0, false
	}

	out, ok := run(ctx, 5*time.Second, "vm_stat")
	if !ok {
		return 0, false
	}
	pages, pageSize, ok := parseVMStat(out)
	if !ok {
		return 0, false
	}

	anonymous := pages["Anonymous pages"] - pages["Pages purgeable"]
	if anonymous < 0 {
		anonymous = 0
	}
	used := (anonymous + pages["Pages wired down"] + pages["Pages occupied by compressor"]) * pageSize
	return percentUsed(float64(used), float64(total))
}

// parseVMStat reads vm_stat's "Key: 1234." lines and its page size.
func parseVMStat(out string) (pages map[string]int64, pageSize int64, ok bool) {
	pages = map[string]int64{}

	for line := range strings.Lines(out) {
		// "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
		if _, rest, found := strings.Cut(line, "(page size of "); found {
			size, _, _ := strings.Cut(rest, " ")
			pageSize, _ = strconv.ParseInt(size, 10, 64)
			continue
		}

		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		n, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(value), "."), 10, 64)
		if err != nil {
			continue
		}
		pages[strings.TrimSpace(key)] = n
	}

	// Without a page size the counts mean nothing, and without the wired
	// count the format is not the one this parses.
	if pageSize <= 0 {
		return nil, 0, false
	}
	if _, found := pages["Pages wired down"]; !found {
		return nil, 0, false
	}
	return pages, pageSize, true
}

// newCPUMeter returns a meter that samples with iostat.
func newCPUMeter() cpuMeter { return iostatMeter{} }

// iostatMeter measures utilization over a one-second sample.
//
// macOS has no counter to difference: there is no /proc/stat, and
// kern.cp_time does not exist here the way it does on the BSDs — the
// numbers live behind host_processor_info, which is a Mach call and so
// needs cgo. Runnerly does not build with cgo, and adding it to put a
// percentage on a dashboard is not a trade worth making.
//
// So this shells out, which is why readings are taken in the background
// rather than while a heartbeat waits. iostat is used rather than top
// because it costs about a hundredth of the processor time for the same
// answer, and prints fixed columns.
type iostatMeter struct{}

func (iostatMeter) percent(ctx context.Context) (float64, bool) {
	// Two samples, one second apart: the first covers the time since boot
	// and is discarded, the second is the second that just passed.
	out, ok := run(ctx, 10*time.Second, "iostat", "-c", "2", "-w", "1")
	if !ok {
		return 0, false
	}
	return parseIostat(out)
}

// parseIostat pulls the idle percentage out of iostat's last row.
//
// The disk columns vary with how many disks are attached, so the header is
// read to find which column idle is in rather than counting from an end.
func parseIostat(out string) (float64, bool) {
	var idleColumn = -1
	var last []string

	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if column := indexOf(fields, "id"); column >= 0 {
			idleColumn = column
			continue
		}
		last = fields
	}

	if idleColumn < 0 || idleColumn >= len(last) {
		return 0, false
	}
	idle, err := strconv.ParseFloat(last[idleColumn], 64)
	if err != nil {
		return 0, false
	}
	return 100 - idle, true
}

func indexOf(fields []string, want string) int {
	for i, field := range fields {
		if field == want {
			return i
		}
	}
	return -1
}

// run executes a command and returns its standard output.
func run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	//nolint:gosec // every caller passes a literal command with literal arguments
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}
