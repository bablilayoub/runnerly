//go:build windows

package supervisor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Windows has no signals, so a graceful stop is a console control event.
// The runner is a console program and treats Ctrl+Break the way it treats
// SIGTERM elsewhere: finish the job in flight, then shut down.
var (
	supervisorKernel32          = syscall.NewLazyDLL("kernel32.dll")
	procGenerateConsoleCtrlEvnt = supervisorKernel32.NewProc("GenerateConsoleCtrlEvent")
	procAttachConsole           = supervisorKernel32.NewProc("AttachConsole")
	procFreeConsole             = supervisorKernel32.NewProc("FreeConsole")
	procSetConsoleCtrlHandler   = supervisorKernel32.NewProc("SetConsoleCtrlHandler")
)

// ctrlBreakEvent is CTRL_BREAK_EVENT.
//
// Not CTRL_C_EVENT: Windows disables Ctrl+C for a process created with
// CREATE_NEW_PROCESS_GROUP, which is exactly how the child is created
// here, so the one that can be delivered is Break.
const ctrlBreakEvent = 1

// setProcessGroup puts the child in its own process group, so a control
// event can be sent to the runner and everything it started without being
// sent to the agent supervising them.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// signalGroup delivers a console control event to the child's group.
//
// Only the graceful stop goes this way. A kill has no console equivalent,
// so it falls through to the caller's TerminateProcess.
func signalGroup(pid int, sig os.Signal) error {
	if sig != os.Interrupt {
		return errors.New("only a graceful stop can be sent as a console event")
	}
	return sendCtrlBreak(uint32(pid)) //nolint:gosec // a pid is not negative
}

// sendCtrlBreak sends CTRL_BREAK_EVENT to a process group.
//
// A process without a console of its own cannot send one, which is the
// case that matters: under a service there is no console, and a runner
// that could only ever be killed would lose the job it was running. So
// when the first attempt fails, this borrows the child's console, sends
// the event and hands it back.
//
// Borrowing one means this process briefly has a console that would
// deliver the same event to itself. The handler is disabled around it for
// that reason — without that, stopping the runner stops the agent.
func sendCtrlBreak(pid uint32) error {
	if err := generateCtrlBreak(pid); err == nil {
		return nil
	}

	if ret, _, err := procAttachConsole.Call(uintptr(pid)); ret == 0 {
		return fmt.Errorf("attach to the runner's console: %w", err)
	}
	defer func() { _, _, _ = procFreeConsole.Call() }()

	// TRUE with a nil handler means "ignore Ctrl+C and Ctrl+Break in this
	// process", which is what keeps the agent alive through the next line.
	_, _, _ = procSetConsoleCtrlHandler.Call(0, 1)
	defer func() { _, _, _ = procSetConsoleCtrlHandler.Call(0, 0) }()

	return generateCtrlBreak(pid)
}

func generateCtrlBreak(pid uint32) error {
	ret, _, err := procGenerateConsoleCtrlEvnt.Call(uintptr(ctrlBreakEvent), uintptr(pid))
	if ret == 0 {
		return fmt.Errorf("send Ctrl+Break to %d: %w", pid, err)
	}
	return nil
}

// terminateSignal is what Stop asks for. On Windows os.Interrupt is not a
// signal that can be delivered to a process at all; it is the value that
// selects the console event above.
func terminateSignal() os.Signal { return os.Interrupt }
