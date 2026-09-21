//go:build linux

package agent

import (
	"os"
	"strconv"
	"strings"
)

// memoryTotal reads MemTotal out of /proc/meminfo.
//
// There is no portable way to ask the standard library for physical
// memory, and neither platform's answer is worth a dependency, so each
// reads its own. A machine that will not say reports 0, which every
// surface renders as unknown rather than as zero bytes.
func memoryTotal() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	for line := range strings.Lines(string(data)) {
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		// "MemTotal:       16316200 kB"
		fields := strings.Fields(rest)
		if len(fields) < 1 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
