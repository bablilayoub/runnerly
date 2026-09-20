package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The security-relevant guarantee: without a terminal to turn echo off on,
// ReadSecret refuses rather than prompting visibly. A token in a scrollback
// buffer, a CI log or a screen recording is worse than being told to pipe it
// in.
func TestReadSecretRefusesRatherThanEchoing(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   func(t *testing.T) *os.File
	}{
		{
			name: "a pipe",
			in: func(t *testing.T) *os.File {
				r, w, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { r.Close(); w.Close() })
				go func() { _, _ = w.WriteString("ghp_secret\n"); w.Close() }()
				return r
			},
		},
		{
			name: "a file",
			in: func(t *testing.T) *os.File {
				path := t.TempDir() + "/token"
				if err := os.WriteFile(path, []byte("ghp_secret\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				f, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { f.Close() })
				return f
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := ReadSecret(tc.in(t), &out, "Token: ")

			if !errors.Is(err, ErrNoSecretPrompt) {
				t.Fatalf("error = %v, want ErrNoSecretPrompt", err)
			}
			if got != "" {
				t.Errorf("it read %q anyway", got)
			}
			// Nothing was written, so no prompt appeared next to input that
			// would have been visible.
			if out.Len() != 0 {
				t.Errorf("it prompted anyway: %q", out.String())
			}
		})
	}
}

// A reader that is not a file at all cannot be a terminal.
func TestReadSecretRefusesANonFileReader(t *testing.T) {
	var out bytes.Buffer
	if _, err := ReadSecret(strings.NewReader("ghp_secret\n"), &out, "Token: "); !errors.Is(err, ErrNoSecretPrompt) {
		t.Fatalf("error = %v, want ErrNoSecretPrompt", err)
	}
}

// secretHelperEnv turns this test binary into the program under test, which
// is how the standard library tests things that need a real process. Here it
// is needed because the behavior only exists on a terminal, and a terminal
// has to be allocated by something outside the process.
const secretHelperEnv = "RUNNERLY_SECRET_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(secretHelperEnv) == "1" {
		got, err := ReadSecret(os.Stdin, os.Stdout, "Token: ")
		if err != nil {
			os.Stdout.WriteString("HELPER-ERR " + err.Error() + "\n")
			os.Exit(1)
		}
		os.Stdout.WriteString("HELPER-READ[" + got + "]\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// On a real terminal the secret is read and never echoed. `script` is what
// allocates the pty, so that no terminal package becomes a dependency for
// one syscall.
func TestReadSecretOnATerminalDoesNotEcho(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script is not installed, so no pty can be allocated")
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// A pty that never yields must not hang the suite.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// script's own arguments differ between the BSD and util-linux versions.
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "script", "-q", "/dev/null", self,
			"-test.run", "TestReadSecretOnATerminalDoesNotEcho")
	case "linux":
		cmd = exec.CommandContext(ctx, "script", "-q", "-c",
			self+" -test.run=TestReadSecretOnATerminalDoesNotEcho", "/dev/null")
	default:
		t.Skip("no known script invocation for " + runtime.GOOS)
	}

	const secret = "ghp_notarealtoken"
	cmd.Env = append(os.Environ(), secretHelperEnv+"=1")

	// The secret is typed only once the prompt has appeared. Writing it
	// immediately would let the terminal echo it before the program had a
	// chance to turn echo off, which says nothing about the program: a
	// person types after being asked.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var seen bytes.Buffer
	typed := make(chan struct{})
	go func() {
		defer close(typed)
		buf := make([]byte, 256)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				seen.Write(buf[:n])
				if strings.Contains(seen.String(), "Token: ") {
					_, _ = stdin.Write([]byte(secret + "\n"))
					break
				}
			}
			if err != nil {
				return
			}
		}
		// Drain the rest so the child is never blocked writing.
		_, _ = io.Copy(&seen, stdout)
	}()

	<-typed
	rest, _ := io.ReadAll(stdout)
	seen.Write(rest)
	_ = stdin.Close()
	err = cmd.Wait()

	text := seen.String()
	if err != nil && !strings.Contains(text, "HELPER-READ") {
		t.Skipf("script did not give a usable pty here: %v\n%s", err, text)
	}

	if !strings.Contains(text, "HELPER-READ["+secret+"]") {
		t.Fatalf("the secret was not read back:\n%s", text)
	}

	// The prompt is echoed; the typed secret must not be. It appears once,
	// inside HELPER-READ[...], and nowhere else.
	if strings.Count(text, secret) != 1 {
		t.Errorf("the secret appears %d times, so the terminal echoed it:\n%s",
			strings.Count(text, secret), text)
	}
	if !strings.Contains(text, "Token: ") {
		t.Errorf("the prompt was not shown:\n%s", text)
	}
}
