//go:build !unix

package doctor

import "errors"

// diskFree is not implemented where there is no statfs. The caller reports
// the check as skipped rather than guessing.
func diskFree(string) (free, total uint64, err error) {
	return 0, 0, errors.New("reading free disk space is not implemented on this platform")
}
