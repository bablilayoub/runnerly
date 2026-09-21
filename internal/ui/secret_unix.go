//go:build !windows

package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// sttyTimeout bounds each stty call. It should return in microseconds; a
// bound exists so a wedged terminal cannot hang the prompt forever, and is
// generous enough that a loaded machine does not trip it.
const sttyTimeout = 5 * time.Second

// disableEcho turns off terminal echo and returns a function restoring the
// previous state. The restore runs even on a signal-driven exit, because the
// alternative is leaving the operator with a terminal that does not echo.
func disableEcho(tty *os.File) (func(), error) {
	saved, err := stty(tty, "-g")
	if err != nil {
		return nil, err
	}
	if _, err := stty(tty, "-echo"); err != nil {
		return nil, err
	}
	return func() { _, _ = stty(tty, saved) }, nil
}

func stty(tty *os.File, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sttyTimeout)
	defer cancel()

	// #nosec G204 -- every argument is a constant here or stty's own saved
	// state read back from stty itself. Nothing reaches a shell: the values
	// are separate argv entries, so there is nothing to quote or escape.
	cmd := exec.CommandContext(ctx, "stty", args...)
	cmd.Stdin = tty
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("stty %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
