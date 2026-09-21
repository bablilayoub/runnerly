//go:build !unix && !windows

package machine

// DiskTotal reports unknown: disk size is read through statfs, which this
// platform does not have. Every surface renders 0 as a dash.
func DiskTotal(string) int64 { return 0 }

func diskUsedPercent(string) (float64, bool) { return 0, false }
