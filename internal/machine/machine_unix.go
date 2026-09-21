//go:build unix

package machine

import "syscall"

// DiskTotal returns the size in bytes of the filesystem holding path.
//
// Total rather than free, because this is reported once at enrollment to
// describe the machine. How full it is right now is a heartbeat's job.
func DiskTotal(path string) int64 {
	stat, ok := statfs(path)
	if !ok {
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

// diskUsedPercent reports how full the filesystem holding path is.
//
// Used is everything not free, measured against only the headroom an
// unprivileged process could actually have: blocks reserved for root count
// as used, because a build cannot write to them. Block size cancels out,
// so it is not read here.
//
// On Linux this is the number df prints. On macOS it is not, and the
// difference is worth knowing: statfs describes the whole APFS container,
// while df splits it per volume. For a 460 GiB disk with 235 GiB free, df
// says the data volume is 46% full and this says the disk is 49% full.
// The second is the one that answers whether a build will fit.
func diskUsedPercent(path string) (float64, bool) {
	stat, ok := statfs(path)
	if !ok {
		return 0, false
	}
	blocks := uint64(stat.Blocks) //nolint:unconvert // signed on some platforms
	free := uint64(stat.Bfree)    //nolint:unconvert,gosec // signed on some platforms
	avail := uint64(stat.Bavail)  //nolint:unconvert,gosec // signed on some platforms
	if blocks == 0 || free > blocks {
		return 0, false
	}

	used := blocks - free
	return percentUsed(float64(used), float64(used+avail))
}

// statfs asks the kernel about the filesystem holding path.
func statfs(path string) (syscall.Statfs_t, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return stat, false
	}
	return stat, true
}
