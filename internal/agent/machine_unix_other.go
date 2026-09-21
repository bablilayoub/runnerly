//go:build unix && !linux && !darwin

package agent

// Another unix, where statfs works but neither memory source does.
func memoryTotal() int64 { return 0 }
