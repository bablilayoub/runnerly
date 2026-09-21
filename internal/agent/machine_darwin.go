//go:build darwin

package agent

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// memoryTotal asks the kernel for physical memory.
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
func memoryTotal() int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return size
}
