// Package ui renders Runnerly's terminal output.
//
// It deliberately has no dependencies: colors are plain ANSI SGR codes and
// terminal detection is a stat() on the underlying file. Color is disabled
// automatically when the writer is not a terminal, when TERM=dumb, or when
// NO_COLOR is set (https://no-color.org).
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Symbols used for check results. Chosen to stay readable without color.
const (
	SymbolPass = "✓" // ✓
	SymbolFail = "✗" // ✗
	SymbolWarn = "!"
	SymbolSkip = "○" // ○
)

type color string

const (
	reset     color = "\033[0m"
	bold      color = "\033[1m"
	dim       color = "\033[2m"
	red       color = "\033[31m"
	green     color = "\033[32m"
	yellow    color = "\033[33m"
	noColor   color = ""
	indentPad       = "  "
)

// Printer writes styled output to a writer.
type Printer struct {
	w     io.Writer
	color bool
}

// New returns a Printer for w. Color support is detected from the
// environment and from w itself.
func New(w io.Writer) *Printer {
	return &Printer{w: w, color: colorEnabled(w, os.Getenv)}
}

// NewPlain returns a Printer that never emits escape codes. Used for
// --no-color and for tests.
func NewPlain(w io.Writer) *Printer {
	return &Printer{w: w, color: false}
}

// SetColor forces color on or off, overriding detection.
func (p *Printer) SetColor(on bool) { p.color = on }

// Color reports whether the printer emits escape codes.
func (p *Printer) Color() bool { return p.color }

// Writer returns the underlying writer.
func (p *Printer) Writer() io.Writer { return p.w }

func (p *Printer) paint(c color, s string) string {
	if !p.color || c == noColor {
		return s
	}
	return string(c) + s + string(reset)
}

// Printf writes unstyled formatted text.
func (p *Printer) Printf(format string, args ...any) {
	fmt.Fprintf(p.w, format, args...)
}

// Println writes an unstyled line.
func (p *Printer) Println(args ...any) {
	fmt.Fprintln(p.w, args...)
}

// Heading writes a bold heading followed by a blank line.
func (p *Printer) Heading(s string) {
	fmt.Fprintf(p.w, "%s\n\n", p.paint(bold, s))
}

// Dim writes dimmed text on its own line.
func (p *Printer) Dim(format string, args ...any) {
	fmt.Fprintln(p.w, p.paint(dim, fmt.Sprintf(format, args...)))
}

// Pass writes a successful check line.
func (p *Printer) Pass(format string, args ...any) {
	p.status(green, SymbolPass, fmt.Sprintf(format, args...))
}

// Fail writes a failed check line.
func (p *Printer) Fail(format string, args ...any) {
	p.status(red, SymbolFail, fmt.Sprintf(format, args...))
}

// Warn writes a check line that neither passed nor blocks.
func (p *Printer) Warn(format string, args ...any) {
	p.status(yellow, SymbolWarn, fmt.Sprintf(format, args...))
}

// Skip writes a check line that was not run.
func (p *Printer) Skip(format string, args ...any) {
	p.status(dim, SymbolSkip, fmt.Sprintf(format, args...))
}

func (p *Printer) status(c color, symbol, msg string) {
	fmt.Fprintf(p.w, "%s %s\n", p.paint(c, symbol), msg)
}

// Detail writes indented explanatory text under a check line. Blank input is
// ignored so callers do not need to branch.
func (p *Printer) Detail(s string) {
	for _, line := range splitLines(s) {
		fmt.Fprintf(p.w, "%s%s\n", indentPad, p.paint(dim, line))
	}
}

// Remedy writes an indented, actionable suggestion under a check line.
func (p *Printer) Remedy(s string) {
	lines := splitLines(s)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(p.w, "%sTry:\n", indentPad)
	for _, line := range lines {
		fmt.Fprintf(p.w, "%s%s%s\n", indentPad, indentPad, p.paint(bold, line))
	}
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func colorEnabled(w io.Writer, getenv func(string) string) bool {
	if getenv("NO_COLOR") != "" {
		return false
	}
	if getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(w)
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
