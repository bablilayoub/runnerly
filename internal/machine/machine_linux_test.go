//go:build linux

package machine

import "testing"

// Captured from /proc/stat on Ubuntu 24.04.
const procStatOutput = `cpu  120000 500 40000 900000 3000 0 1200 0 0 0
cpu0 60000 250 20000 450000 1500 0 600 0 0 0
intr 123456789
ctxt 987654321
`

func TestParseProcStatIgnoresGuestTime(t *testing.T) {
	busy, total, ok := parseProcStat(procStatOutput)
	if !ok {
		t.Fatal("parseProcStat rejected real /proc/stat output")
	}
	// user+nice+system+irq+softirq+steal, with idle and iowait left out.
	const wantBusy = 120000 + 500 + 40000 + 0 + 1200 + 0
	const wantTotal = wantBusy + 900000 + 3000
	if busy != wantBusy || total != wantTotal {
		t.Errorf("busy, total = %d, %d; want %d, %d", busy, total, wantBusy, wantTotal)
	}
}

// guest and guest_nice are already inside user and nice. Counting them
// again would put a busy virtual machine over 100%.
func TestParseProcStatDoesNotDoubleCountGuests(t *testing.T) {
	withGuests := "cpu  100 0 0 100 0 0 0 0 50 25\n"
	busy, total, ok := parseProcStat(withGuests)
	if !ok {
		t.Fatal("parseProcStat rejected the line")
	}
	if busy != 100 || total != 200 {
		t.Errorf("busy, total = %d, %d; want 100, 200", busy, total)
	}
}

func TestParseProcStatRejectsSomethingElse(t *testing.T) {
	for name, out := range map[string]string{
		"empty":       "",
		"wrong line":  "intr 12345\ncpu 1 2 3 4 5\n",
		"too short":   "cpu  1 2 3\n",
		"not numbers": "cpu  a b c d e\n",
	} {
		if _, _, ok := parseProcStat(out); ok {
			t.Errorf("%s: parseProcStat claimed to understand it", name)
		}
	}
}

const meminfoOutput = `MemTotal:       16316200 kB
MemFree:          401232 kB
MemAvailable:   12043928 kB
Buffers:          123456 kB
`

func TestParseMeminfo(t *testing.T) {
	total, ok := parseMeminfo(meminfoOutput, "MemTotal:")
	if !ok || total != 16316200*1024 {
		t.Errorf("MemTotal = %d, %v", total, ok)
	}
	available, ok := parseMeminfo(meminfoOutput, "MemAvailable:")
	if !ok || available != 12043928*1024 {
		t.Errorf("MemAvailable = %d, %v", available, ok)
	}
	// MemFree is a prefix of nothing else, but Mem is a prefix of all of
	// them: a key has to match up to its colon.
	if _, ok := parseMeminfo(meminfoOutput, "Mem:"); ok {
		t.Error("a partial key matched")
	}
	if _, ok := parseMeminfo(meminfoOutput, "Shmem:"); ok {
		t.Error("an absent key matched")
	}
}

// Memory use is MemAvailable, not MemFree. On a machine that has been up a
// while those differ by an order of magnitude, because the kernel spends
// everything spare on page cache it gives back on demand. Reporting free
// memory would show every healthy build machine at 97% used.
func TestMemoryUseIsMeasuredAgainstAvailableNotFree(t *testing.T) {
	total, _ := parseMeminfo(meminfoOutput, "MemTotal:")
	available, _ := parseMeminfo(meminfoOutput, "MemAvailable:")
	free, _ := parseMeminfo(meminfoOutput, "MemFree:")

	fromAvailable, _ := percentUsed(float64(total-available), float64(total))
	fromFree, _ := percentUsed(float64(total-free), float64(total))

	if fromAvailable > 30 {
		t.Errorf("used from MemAvailable = %.0f%%, want the roomy answer", fromAvailable)
	}
	if fromFree < 90 {
		t.Errorf("used from MemFree = %.0f%%, want the alarming one", fromFree)
	}
}

// The readings have to come from the machine actually running the test.
func TestLinuxReadsThisMachine(t *testing.T) {
	if total := MemoryTotal(); total < 1<<26 {
		t.Errorf("MemoryTotal = %d, which is implausibly small", total)
	}

	used, ok := memoryUsedPercent(t.Context())
	if !ok {
		t.Fatal("could not read memory use on the machine running the test")
	}
	if used <= 0 || used > 100 {
		t.Errorf("memory %.1f%% used, which is not believable", used)
	}

	meter := newCPUMeter()
	if _, ok := meter.percent(t.Context()); !ok {
		// Constructing the meter primes it, so the first call after that
		// has a window, however short.
		t.Skip("the two readings landed in the same tick")
	}
}
