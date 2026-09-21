package machine

import (
	"context"
	"testing"
	"time"
)

func TestClampKeepsPercentagesInRange(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{-1, 0}, {0, 0}, {50, 50}, {100, 100}, {4000, 100},
	} {
		if got := clamp(tc.in); got != tc.want {
			t.Errorf("clamp(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPercentUsedRejectsNonsense(t *testing.T) {
	if _, ok := percentUsed(1, 0); ok {
		t.Error("a zero total should be unknown, not a division")
	}
	if _, ok := percentUsed(-1, 10); ok {
		t.Error("negative use should be unknown")
	}
	got, ok := percentUsed(25, 200)
	if !ok || got != 12.5 {
		t.Errorf("percentUsed(25, 200) = %v, %v; want 12.5, true", got, ok)
	}
}

// A sampler that has not run yet reports nothing rather than zero load.
// The distinction is the whole point: the dashboard renders 0 as a dash.
func TestLatestBeforeFirstRefreshIsUnknown(t *testing.T) {
	s := NewSampler(t.TempDir())
	if load := s.Latest(); load != (Load{}) {
		t.Errorf("a sampler that has not sampled reported %+v", load)
	}
}

func TestRunStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		NewSampler(t.TempDir()).Run(ctx, time.Hour)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when its context ended")
	}
}

// Run takes its first reading well before the interval, so the first
// heartbeat carries numbers instead of dashes.
func TestRunSamplesBeforeTheFirstInterval(t *testing.T) {
	s := NewSampler(t.TempDir())
	s.cpu = fixedCPU{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.Run(ctx, time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.Latest().CPUPercent == 42 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Run never stored a reading")
}

type fixedCPU struct{}

func (fixedCPU) percent(context.Context) (float64, bool) { return 42, true }

// Disk is the one reading every unix can take, so it is the one that can
// be asserted against a real filesystem without a platform guard.
func TestDiskReadingsDescribeARealFilesystem(t *testing.T) {
	dir := t.TempDir()

	total := DiskTotal(dir)
	if total <= 0 {
		t.Fatalf("DiskTotal(%q) = %d, want a positive size", dir, total)
	}

	used, ok := diskUsedPercent(dir)
	if !ok {
		t.Fatal("diskUsedPercent could not read a filesystem it just wrote to")
	}
	if used <= 0 || used > 100 {
		t.Errorf("disk %.1f%% full, which is not a believable answer", used)
	}
}

func TestDiskOfAMissingPathIsUnknown(t *testing.T) {
	missing := t.TempDir() + "/nowhere"
	if total := DiskTotal(missing); total != 0 {
		t.Errorf("DiskTotal of a missing path = %d, want 0", total)
	}
	if _, ok := diskUsedPercent(missing); ok {
		t.Error("diskUsedPercent of a missing path claimed to know")
	}
}
