//go:build !unix

package supervisor

import (
	"errors"
	"os"
	"os/exec"
)

// setProcessGroup is a no-op where process groups are not available.
func setProcessGroup(*exec.Cmd) {}

// signalGroup always fails here, so the caller signals the process itself.
func signalGroup(int, os.Signal) error {
	return errors.New("process groups are not supported on this platform")
}

// terminateSignal falls back to interrupt.
func terminateSignal() os.Signal { return os.Interrupt }
