package ui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// sttyTimeout bounds each stty call. It should return in microseconds; a
// bound exists so a wedged terminal cannot hang the prompt forever, and is
// generous enough that a loaded machine does not trip it.
const sttyTimeout = 5 * time.Second

// ErrNoSecretPrompt is returned when a secret cannot be read without echoing
// it. Callers turn it into advice for piping the value in instead.
var ErrNoSecretPrompt = errors.New("cannot read a secret without echoing it")

// ReadSecret prompts on out and reads one line from in with terminal echo
// turned off.
//
// Echo is disabled by shelling out to stty rather than by adding a terminal
// dependency: this is the only place in Runnerly that needs it, and a package
// for it would be the fourth direct dependency for one syscall.
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
