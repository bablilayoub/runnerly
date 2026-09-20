//go:build unix

package doctor

import "syscall"

// diskFree returns the free and total bytes of the filesystem holding path.
//
// It uses the space available to an unprivileged user rather than the raw
// free space, because the reserved blocks a filesystem keeps for root are
// not space a runner can fill.
func diskFree(path string) (free, total uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	size := uint64(stat.Bsize) //nolint:gosec,unconvert // Bsize is int64 on some platforms and uint32 on others
	return stat.Bavail * size, stat.Blocks * size, nil
}
