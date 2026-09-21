package cli

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentLaunchdRendersAPlist(t *testing.T) {
	cfg := configIn(t, "")
	r := recordInstalled(t, cfg, "runnerly-01")

	out, _, err := runCLI(t, nil, "--config", cfg, "agent", "launchd", "runnerly-01")
	if err != nil {
		t.Fatalf("agent launchd: %v", err)
	}

	for _, want := range []string{
		`<plist version="1.0">`,
		"<string>dev.runnerly.agent.runnerly-01</string>",
		"<string>--name</string>",
		"<string>runnerly-01</string>",
		"<string>" + r.Dir + "</string>",
		"<key>KeepAlive</key>",
		"<key>RunAtLoad</key>",
		"<key>ExitTimeOut</key>",
		// launchd kills after twenty seconds by default, which would throw
		// away whatever the runner was building.
		"<integer>180</integer>",
		"<key>SessionCreate</key>",
		"/opt/homebrew/bin",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q:\n%s", want, out)
		}
	}

	// It prints the commands rather than running them, and none need root.
	for _, want := range []string{"launchctl bootstrap", "launchctl bootout", "tail -f"} {
		if !strings.Contains(out, want) {
			t.Errorf("instructions missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "sudo ") {
		t.Errorf("a LaunchAgent needs no root:\n%s", out)
	}
	// A LaunchAgent only runs while someone is logged in, which is the one
	// thing about this that surprises people.
	if !strings.Contains(out, "logged in") {
		t.Errorf("the login caveat is missing:\n%s", out)
	}
}

// The plist has to be a plist. Paths come from a configuration file and a
// home directory, and both can legally contain characters XML cannot carry
// raw — launchd rejects the whole file with a line number rather than a
// cause.
func TestAgentLaunchdEscapesPathsIntoValidXML(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")

	awkward := filepath.Join(t.TempDir(), "logs & more", "<agent>.log")
	out, _, err := runCLI(t, nil, "--config", cfg,
		"agent", "launchd", "runnerly-01", "--log-file", awkward)
	if err != nil {
		t.Fatalf("agent launchd: %v", err)
	}

	plist := out[strings.Index(out, "<?xml"):]
	plist = plist[:strings.Index(plist, "</plist>")+len("</plist>")]

	if strings.Contains(plist, "logs & more") {
		t.Error("an ampersand went into the plist raw")
	}
	if err := xml.Unmarshal([]byte(plist), new(any)); err != nil {
		t.Errorf("the plist is not well-formed XML: %v\n%s", err, plist)
	}
}

func TestAgentLaunchdWritesToAFile(t *testing.T) {
	cfg := configIn(t, "")
	recordInstalled(t, cfg, "runnerly-01")
	dest := filepath.Join(t.TempDir(), "dev.runnerly.agent.runnerly-01.plist")

	out, _, err := runCLI(t, nil, "--config", cfg,
		"agent", "launchd", "runnerly-01", "--output", dest)
	if err != nil {
		t.Fatalf("agent launchd --output: %v", err)
	}

	written, err := os.ReadFile(dest) //nolint:gosec // a path this test made
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "dev.runnerly.agent.runnerly-01") {
		t.Errorf("the file does not hold the plist:\n%s", written)
	}
	// Standard output gets the instructions, not a second copy.
	if strings.Contains(out, "<?xml") {
		t.Errorf("the plist was printed as well as written:\n%s", out)
	}
	if !strings.Contains(out, dest) {
		t.Errorf("the instructions do not name the file that was written:\n%s", out)
	}
}

func TestEphemeralLaunchdRendersAPlist(t *testing.T) {
	cfg := configIn(t, "github:\n  repository: acme/widgets\n")

	out, _, err := runCLI(t, nil, "--config", cfg, "ephemeral", "launchd")
	if err != nil {
		t.Fatalf("ephemeral launchd: %v", err)
	}

	for _, want := range []string{
		"<string>dev.runnerly.ephemeral</string>",
		"<string>ephemeral</string>",
		"<string>run</string>",
		"<string>acme/widgets</string>",
		// KeepAlive is the whole scheduler: one job, exit, start another.
		"<key>KeepAlive</key>",
		"<key>ThrottleInterval</key>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q:\n%s", want, out)
		}
	}
}
