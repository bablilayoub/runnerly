//go:build windows

package machine

import (
	"context"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Windows has no /proc and no statfs. Every reading here comes from
// kernel32, called directly rather than through a dependency: three calls
// do not justify one, and the alternative — shelling out to wmic or
// PowerShell on every heartbeat — costs a process each time and has been
// deprecated once already.
//
// kernel32 is a KnownDLL, so it is always resolved from the system
// directory and already loaded in every process.
var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	getDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
	getSystemTimes       = kernel32.NewProc("GetSystemTimes")
)

// memoryStatusEx mirrors MEMORYSTATUSEX. The field order and the Length
// field are part of the calling convention: the kernel reads Length to
// decide which version of the struct it was handed.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func memoryStatus() (memoryStatusEx, bool) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))

	ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	return status, ret != 0
}

// MemoryTotal returns physical memory in bytes, or 0 if it cannot be read.
func MemoryTotal() int64 {
	status, ok := memoryStatus()
	if !ok || status.TotalPhys > 1<<62 {
		return 0
	}
	return int64(status.TotalPhys)
}

// memoryUsedPercent reports how much memory is committed.
//
// MemoryLoad is the kernel's own answer to that question, and it is the
// number Task Manager shows, so an operator can check it against the
// machine. Deriving one from TotalPhys and AvailPhys would be a second
// definition that disagrees with the first at the edges.
func memoryUsedPercent(context.Context) (float64, bool) {
	status, ok := memoryStatus()
	if !ok {
		return 0, false
	}
	return float64(status.MemoryLoad), true
}

// DiskTotal returns the size in bytes of the volume holding path.
func DiskTotal(path string) int64 {
	_, total, _, ok := diskSpace(path)
	if !ok || total > 1<<62 {
		return 0
	}
	return int64(total)
}

// diskUsedPercent reports how full the volume holding path is.
//
// Measured against what this user could actually write, not the raw size:
// a quota is the Windows equivalent of the blocks unix reserves for root,
// and space a build cannot have is not headroom.
func diskUsedPercent(path string) (float64, bool) {
	availableToCaller, total, free, ok := diskSpace(path)
	if !ok || total == 0 || free > total {
		return 0, false
	}

	used := total - free
	return percentUsed(float64(used), float64(used+availableToCaller))
}

// diskSpace calls GetDiskFreeSpaceExW for the volume holding path.
//
// The path has to exist and has to be a directory the caller can see. A
// runner directory that is not there yet reports unknown, the same as
// everywhere else.
func diskSpace(path string) (availableToCaller, total, free uint64, ok bool) {
	if path == "" {
		return 0, 0, 0, false
	}
	// GetDiskFreeSpaceExW wants a directory. Handing it a file path works
	// on some versions and not others, so resolve to something it always
	// accepts.
	dir, err := syscall.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return 0, 0, 0, false
	}

	ret, _, _ := getDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(dir)),
		uintptr(unsafe.Pointer(&availableToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&free)),
	)
	return availableToCaller, total, free, ret != 0
}

// newCPUMeter returns a meter that differences the kernel's own counters.
func newCPUMeter() cpuMeter {
	m := &systemTimesMeter{}
	// Prime it, so the first real reading has something to subtract.
	m.percent(context.Background())
	return m
}

// systemTimesMeter measures utilization from GetSystemTimes, the same way
// /proc/stat is differenced on Linux: the window is the time between
// calls.
type systemTimesMeter struct {
	prevIdle, prevTotal uint64
	primed              bool
}

func (m *systemTimesMeter) percent(context.Context) (float64, bool) {
	idle, total, ok := systemTimes()
	if !ok {
		return 0, false
	}
	defer func() {
		m.prevIdle, m.prevTotal, m.primed = idle, total, true
	}()

	if !m.primed || total <= m.prevTotal {
		return 0, false
	}
	deltaTotal := total - m.prevTotal
	deltaIdle := idle - m.prevIdle
	if deltaIdle > deltaTotal {
		// Counters that went backwards, across a suspend or a core coming
		// online. Unknown beats a number above 100.
		return 0, false
	}
	return percentUsed(float64(deltaTotal-deltaIdle), float64(deltaTotal))
}

// systemTimes returns cumulative idle and total 100ns ticks since boot.
//
// The kernel time GetSystemTimes reports already includes the idle time,
// which is the trap in this API: total is kernel plus user, and idle is a
// part of kernel rather than something to add on.
func systemTimes() (idle, total uint64, ok bool) {
	var idleTime, kernelTime, userTime syscall.Filetime

	ret, _, _ := getSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if ret == 0 {
		return 0, 0, false
	}

	idle = filetimeTicks(idleTime)
	total = filetimeTicks(kernelTime) + filetimeTicks(userTime)
	return idle, total, true
}

func filetimeTicks(t syscall.Filetime) uint64 {
	return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime)
}
