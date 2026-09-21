//go:build !linux && !darwin && !windows

package machine

import "context"

// MemoryTotal reports unknown. Memory and processor have a source on
// Linux, macOS and Windows; the other unixes have sources nobody has run
// this against, and a number arrived at by reading documentation is
// exactly the kind that turns out to mean something else.
//
// Zero and false mean unknown here, and every surface renders that as a
// dash rather than as a machine with no memory.
func MemoryTotal() int64 { return 0 }

func memoryUsedPercent(context.Context) (float64, bool) { return 0, false }

func newCPUMeter() cpuMeter { return unavailableCPU{} }

// unavailableCPU is the meter for a platform with no source for the answer.
type unavailableCPU struct{}

func (unavailableCPU) percent(context.Context) (float64, bool) { return 0, false }
