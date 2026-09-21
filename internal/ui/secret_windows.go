//go:build windows

package ui

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Windows has no stty. Echo is a bit in the console mode, which is read
// and written through kernel32 — the same DLL the machine readings use,
// and for the same reason: one flag does not justify a dependency.
var (
	secretKernel32     = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = secretKernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = secretKernel32.NewProc("SetConsoleMode")
)

// enableEchoInput is ENABLE_ECHO_INPUT from the console API.
//
// ENABLE_LINE_INPUT has to stay on: without it the console stops handing
// over whole lines, and the prompt would return on the first keystroke.
const enableEchoInput = 0x0004

// disableEcho turns off console echo and returns a function restoring the
// previous mode.
//
// The whole previous mode is saved and put back, not just this one bit: a
// terminal left in a state its owner did not choose is a worse outcome
// than a visible prompt, and this runs on the way out of a command that
// may have been interrupted.
func disableEcho(tty *os.File) (func(), error) {
	handle := syscall.Handle(tty.Fd())

	var mode uint32
	//nolint:gosec // G103: a syscall out-parameter, which is what this is for
	ret, _, err := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if ret == 0 {
		// Not a console — a pipe or a redirect. The caller turns this into
		// advice about piping the value in.
		return nil, fmt.Errorf("read the console mode: %w", err)
	}

	if ret, _, err := procSetConsoleMode.Call(uintptr(handle), uintptr(mode&^enableEchoInput)); ret == 0 {
		return nil, fmt.Errorf("turn off console echo: %w", err)
	}

	return func() {
		_, _, _ = procSetConsoleMode.Call(uintptr(handle), uintptr(mode))
	}, nil
}
