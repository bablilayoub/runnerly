//go:build !unix

package machine

// Disk is read through statfs, which this platform does not have. Zero and
// false mean unknown, which every surface renders as a dash.

func DiskTotal(string) int64 { return 0 }

func diskUsedPercent(string) (float64, bool) { return 0, false }
