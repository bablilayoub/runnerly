//go:build unix

package supervisor

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group so the supervisor
// can signal it and everything it spawns together.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup delivers sig to the child's whole process group. The negative
// pid is what makes kill(2) address the group.
func signalGroup(pid int, sig os.Signal) error {
	s, ok := sig.(syscall.Signal)
	if !ok {
		return syscall.EINVAL
	}
	return syscall.Kill(-pid, s)
}

// terminateSignal is what Stop sends. SIGTERM is what the official runner
// handles to finish its current job and exit.
func terminateSignal() os.Signal { return syscall.SIGTERM }
