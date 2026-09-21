//go:build !unix

package agent

// The machine facts are read through platform calls that do not exist
// here. Zero means unknown, which every surface renders as a dash rather
// than as a number.
func diskTotal(string) int64 { return 0 }
func memoryTotal() int64     { return 0 }
