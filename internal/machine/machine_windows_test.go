//go:build windows

package machine

import "testing"

// These assert against the machine running the test rather than a
// fixture, because the thing that goes wrong with a syscall binding is not
// arithmetic — it is a struct laid out slightly wrong, or a field that
// means something other than its name suggests. Only a real call catches
// that, which is why CI runs this suite on windows-latest.
func TestWindowsReadsThisMachine(t *testing.T) {
	total := MemoryTotal()
	if total < 1<<30 {
		t.Errorf("MemoryTotal = %d, which is less than a gigabyte", total)
	}

	used, ok := memoryUsedPercent(t.Context())
	if !ok {
		t.Fatal("could not read memory use on the machine running the test")
	}
	if used <= 0 || used > 100 {
		t.Errorf("memory %.1f%% used, which is not believable", used)
	}

	// The meter is primed by its constructor, so the call after it has a
	// window, however short.
	meter := newCPUMeter()
	cpu, ok := meter.percent(t.Context())
	if ok && (cpu < 0 || cpu > 100) {
		t.Errorf("processor %.1f%%, which is not a percentage", cpu)
	}
}

// GetSystemTimes reports kernel time with idle already inside it. Adding
// idle on top would make a quiet machine read as more than 100% busy, and
// subtracting it twice would make a busy one read as idle.
func TestSystemTimesIdleIsPartOfTheTotal(t *testing.T) {
	idle, total, ok := systemTimes()
	if !ok {
		t.Fatal("GetSystemTimes failed")
	}
	if idle == 0 || total == 0 {
		t.Fatalf("idle = %d, total = %d; neither should be zero on a booted machine", idle, total)
	}
	if idle > total {
		t.Errorf("idle %d exceeds total %d, so the two are being combined wrongly", idle, total)
	}
}

func TestWindowsDiskReadings(t *testing.T) {
	dir := t.TempDir()

	if total := DiskTotal(dir); total <= 0 {
		t.Errorf("DiskTotal(%q) = %d, want a positive size", dir, total)
	}
	used, ok := diskUsedPercent(dir)
	if !ok {
		t.Fatal("diskUsedPercent could not read a volume it just wrote to")
	}
	if used <= 0 || used > 100 {
		t.Errorf("disk %.1f%% full, which is not a believable answer", used)
	}
}
