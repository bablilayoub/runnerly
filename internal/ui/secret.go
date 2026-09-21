package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrNoSecretPrompt is returned when a secret cannot be read without echoing
// it. Callers turn it into advice for piping the value in instead.
var ErrNoSecretPrompt = errors.New("cannot read a secret without echoing it")

// ReadSecret prompts on out and reads one line from in with terminal echo
// turned off.
//
// How echo is turned off is per platform and lives beside this file: stty
// on unix, the console API on Windows. Neither is a dependency — this is
// the only place in Runnerly that needs it, and a package for it would be
// the fourth direct dependency for one syscall.
//
// If echo cannot be turned off, ReadSecret returns ErrNoSecretPrompt instead
// of falling back to a visible prompt. A token on screen, in a scrollback
// buffer and possibly in a recording is worse than being told to pipe it in.
func ReadSecret(in io.Reader, out io.Writer, prompt string) (string, error) {
	tty, ok := in.(*os.File)
	if !ok || !IsTerminal(tty) {
		return "", ErrNoSecretPrompt
	}

	restore, err := disableEcho(tty)
	if err != nil {
		return "", ErrNoSecretPrompt
	}
	defer restore()

	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	// The newline the user typed was not echoed, so the next thing printed
	// would otherwise land on the prompt line.
	fmt.Fprintln(out)

	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return strings.TrimSpace(line), nil
}
