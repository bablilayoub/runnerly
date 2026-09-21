//go:build !linux && !darwin

package machine

import "context"

// Memory and processor readings have a source on Linux and macOS and
// nowhere else Runnerly builds for. Windows is not a supported runner
// platform at all, and the other unixes have sources that nobody has run
// this against — a number arrived at by reading documentation is exactly
// the kind that turns out to mean something else.
//
// Zero and false mean unknown, and every surface renders that as a dash.

func MemoryTotal() int64 { return 0 }

func memoryUsedPercent(context.Context) (float64, bool) { return 0, false }

func newCPUMeter() cpuMeter { return unavailableCPU{} }

// unavailableCPU is the meter for a platform with no source for the answer.
type unavailableCPU struct{}

func (unavailableCPU) percent(context.Context) (float64, bool) { return 0, false }
