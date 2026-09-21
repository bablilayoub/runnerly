//go:build unix

package agent

import "syscall"

// diskTotal returns the size in bytes of the filesystem holding path.
//
// Total rather than free, because this is reported once at enrollment to
// describe the machine. How full it is right now is a heartbeat's job.
func diskTotal(path string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	size := uint64(stat.Bsize) //nolint:gosec,unconvert // int64 on some platforms, uint32 on others
	total := stat.Blocks * size
	if total > 1<<62 {
		// Nonsense from an exotic filesystem is better reported as unknown
		// than as a number nobody can believe.
		return 0
	}
	return int64(total) //nolint:gosec // bounded above
}
