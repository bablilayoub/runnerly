package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlainPrinterEmitsNoEscapeCodes(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf)
	p.Heading("Runnerly Doctor")
	p.Pass("Docker installed")
	p.Fail("Docker daemon unavailable")
	p.Warn("running on darwin")
	p.Skip("server not configured")
	p.Detail("the daemon is not running")
	p.Remedy("sudo systemctl start docker")

	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Errorf("plain printer emitted escape codes:\n%q", out)
	}
	for _, want := range []string{
		"Runnerly Doctor",
		SymbolPass + " Docker installed",
		SymbolFail + " Docker daemon unavailable",
		SymbolWarn + " running on darwin",
		SymbolSkip + " server not configured",
		"  the daemon is not running",
		"    sudo systemctl start docker",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestColorPrinterWrapsWithSGR(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf)
	p.SetColor(true)
	p.Pass("ok")
	if !strings.Contains(buf.String(), string(green)+SymbolPass+string(reset)) {
		t.Errorf("expected green symbol, got %q", buf.String())
	}
}

func TestDetailAndRemedyIgnoreBlankInput(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf)
	p.Detail("")
	p.Detail("   \n  ")
	p.Remedy("")
	if buf.Len() != 0 {
		t.Errorf("expected no output for blank input, got %q", buf.String())
	}
}

func TestMultiLineRemedyIndentsEveryLine(t *testing.T) {
	var buf bytes.Buffer
	p := NewPlain(&buf)
	p.Remedy("sudo systemctl start docker\nsudo systemctl enable docker")
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (Try: + 2 commands), got %d: %q", len(lines), lines)
	}
	for _, l := range lines[1:] {
		if !strings.HasPrefix(l, "    ") {
			t.Errorf("command line not indented: %q", l)
		}
	}
}

func TestColorEnabledRespectsEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"no_color set", map[string]string{"NO_COLOR": "1"}, false},
		{"dumb terminal", map[string]string{"TERM": "dumb"}, false},
		{"non-terminal writer", map[string]string{"TERM": "xterm"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			// A bytes.Buffer is never a terminal, so detection must be false.
			if got := colorEnabled(&bytes.Buffer{}, getenv); got != tt.want {
				t.Errorf("colorEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTableAlignsColumns(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("NAME", "STATUS", "LABELS")
	tbl.Row("runnerly-01", "online", "linux,x64")
	tbl.Row("a", "busy", "linux")

	NewPlain(&buf).Table(tbl)

	want := strings.Join([]string{
		"NAME         STATUS  LABELS",
		"runnerly-01  online  linux,x64",
		"a            busy    linux",
	}, "\n") + "\n"

	if buf.String() != want {
		t.Errorf("table misaligned:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestTableLeavesNoTrailingWhitespace(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("NAME", "STATUS")
	tbl.Row("runnerly-01", "online")
	tbl.Row("a", "busy")

	NewPlain(&buf).Table(tbl)
	for _, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("line has trailing whitespace: %q", l)
		}
	}
}

func TestEmptyTableWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	NewPlain(&buf).Table(NewTable("NAME"))
	if buf.Len() != 0 {
		t.Errorf("empty table wrote %q", buf.String())
	}
	NewPlain(&buf).Table(nil)
	if buf.Len() != 0 {
		t.Errorf("nil table wrote %q", buf.String())
	}
}
