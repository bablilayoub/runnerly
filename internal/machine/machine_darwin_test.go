//go:build darwin

package machine

import "testing"

// Captured from `vm_stat` on macOS 26, trimmed to the lines that are read.
const vmStatOutput = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                    10615.
Pages active:                                 408262.
Pages inactive:                               406974.
Pages speculative:                               232.
Pages throttled:                                   0.
Pages wired down:                             218859.
Pages purgeable:                               17374.
"Translation faults":                     4307598947.
File-backed pages:                            224471.
Anonymous pages:                              590997.
Pages occupied by compressor:                 485002.
Swapouts:                                    2228391.
`

func TestParseVMStat(t *testing.T) {
	pages, pageSize, ok := parseVMStat(vmStatOutput)
	if !ok {
		t.Fatal("parseVMStat rejected real vm_stat output")
	}
	if pageSize != 16384 {
		t.Errorf("page size = %d, want 16384", pageSize)
	}
	for key, want := range map[string]int64{
		"Anonymous pages":              590997,
		"Pages wired down":             218859,
		"Pages purgeable":              17374,
		"Pages occupied by compressor": 485002,
	} {
		if got := pages[key]; got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
	// The quoted key is the one that would break a naive split.
	if got := pages[`"Translation faults"`]; got != 4307598947 {
		t.Errorf(`"Translation faults" = %d`, got)
	}
}

func TestParseVMStatRejectsSomethingElse(t *testing.T) {
	for name, out := range map[string]string{
		"empty":        "",
		"no page size": "Pages wired down: 1.\n",
		"no counts":    "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n",
	} {
		if _, _, ok := parseVMStat(out); ok {
			t.Errorf("%s: parseVMStat claimed to understand it", name)
		}
	}
}

// Captured from `iostat -c 2 -w 1`. Two disks here; a machine with one or
// three shifts every column, which is why the header is read.
const iostatOutput = `              disk0               disk4       cpu     load average
    KB/t  tps  MB/s     KB/t  tps  MB/s  us sy id   1m   5m   15m
   20.39  174  3.47    52.55    1  0.04   8  4 88  3.78 5.13 5.90
   10.91  116  1.24     0.00    0  0.00  76 24  0  3.57 4.99 5.84
`

func TestParseIostatUsesTheSecondSample(t *testing.T) {
	got, ok := parseIostat(iostatOutput)
	if !ok {
		t.Fatal("parseIostat rejected real iostat output")
	}
	// The second row is the one-second sample: 100 - 0 idle.
	if got != 100 {
		t.Errorf("utilization = %v, want 100 from the second row", got)
	}
}

func TestParseIostatWithOneDisk(t *testing.T) {
	out := `              disk0       cpu     load average
    KB/t  tps  MB/s  us sy id   1m   5m   15m
   20.39  174  3.47   8  4 88  3.78 5.13 5.90
   12.00   14  0.16  17  8 75  3.57 4.99 5.84
`
	got, ok := parseIostat(out)
	if !ok || got != 25 {
		t.Errorf("utilization = %v, %v; want 25, true", got, ok)
	}
}

func TestParseIostatRejectsSomethingElse(t *testing.T) {
	for name, out := range map[string]string{
		"empty":          "",
		"no idle column": "KB/t tps MB/s\n20.39 174 3.47\n",
		"not a number":   "us sy id\n8 4 none\n",
	} {
		if _, ok := parseIostat(out); ok {
			t.Errorf("%s: parseIostat claimed to understand it", name)
		}
	}
}

// The readings have to come from the machine actually running the test,
// not from a fixture: a parser that works on captured output and not on
// live output is the failure this catches.
func TestDarwinReadsThisMachine(t *testing.T) {
	if total := MemoryTotal(); total < 1<<30 {
		t.Errorf("MemoryTotal = %d, which is less than a gigabyte", total)
	}

	used, ok := memoryUsedPercent(t.Context())
	if !ok {
		t.Fatal("could not read memory use on the machine running the test")
	}
	if used <= 0 || used > 100 {
		t.Errorf("memory %.1f%% used, which is not believable", used)
	}

	cpu, ok := newCPUMeter().percent(t.Context())
	if !ok {
		t.Fatal("could not read processor use on the machine running the test")
	}
	if cpu < 0 || cpu > 100 {
		t.Errorf("processor %.1f%%, which is not a percentage", cpu)
	}
}
