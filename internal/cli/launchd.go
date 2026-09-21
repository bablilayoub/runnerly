package cli

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/ui"
)

// launchd is macOS's service manager, and the reason `agent systemd` was not
// the whole story: a Mac that should run a runner after a reboot needs a
// property list, not a unit file.
//
// What is generated is a LaunchAgent rather than a LaunchDaemon, which is a
// real choice and not a shortcut. A daemon starts at boot with no login
// session, and on macOS that costs a build the things Macs are used for:
// the login keychain, code signing, and anything that needs a window server.
// GitHub's own runner installs itself as a LaunchAgent for the same reason.
// The price is that the machine has to log in, which the printed
// instructions say out loud rather than leaving to be discovered.

// launchdDomain is the reverse-DNS prefix for every label Runnerly emits.
const launchdDomain = "dev.runnerly"

// launchdPATH is what a job inherits unless told otherwise.
//
// launchd hands a job a minimal PATH — no Homebrew, no /usr/local/bin — and
// the agent passes its own environment to the runner, so a workflow calling
// anything installed by brew fails with "command not found" on a machine
// where it plainly is installed. This is the single most common way a macOS
// runner that works in a terminal fails as a service.
const launchdPATH = "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// agentPlist is what `agent launchd` emits.
const agentPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xml .Label}}</string>

	<key>ProgramArguments</key>
	<array>
		<string>{{xml .Binary}}</string>
		<string>--name</string>
		<string>{{xml .Runner}}</string>
{{- if .ConfigPath}}
		<string>--config</string>
		<string>{{xml .ConfigPath}}</string>
{{- end}}
	</array>

	<key>WorkingDirectory</key>
	<string>{{xml .Dir}}</string>

	<!-- launchd restarts the agent; the agent restarts the runner. Both are
	     needed: the agent handles a crashing runner, this handles a crashing
	     agent. ThrottleInterval is launchd's RestartSec. -->
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>5</integer>

	<!-- The runner finishes the job it is on when asked to stop. launchd
	     sends SIGTERM and then SIGKILL after this many seconds; the default
	     is twenty, which would throw away a build. -->
	<key>ExitTimeOut</key>
	<integer>{{.StopTimeout}}</integer>

	<!-- A build is not a background task: Standard lets macOS throttle its
	     I/O and CPU, which shows up as a runner that is mysteriously slower
	     than the same commands typed into a terminal. -->
	<key>ProcessType</key>
	<string>Interactive</string>

	<!-- Its own security session, so the login keychain and code signing
	     work. This is what a LaunchDaemon cannot have. -->
	<key>SessionCreate</key>
	<true/>

	<!-- There is no journal on macOS, so the logs go to a file. Nothing
	     rotates it; see the documentation. -->
	<key>StandardOutPath</key>
	<string>{{xml .LogFile}}</string>
	<key>StandardErrorPath</key>
	<string>{{xml .LogFile}}</string>

	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>{{xml .PATH}}</string>
	</dict>
</dict>
</plist>
`

// ephemeralPlist is what `ephemeral launchd` emits.
//
// KeepAlive is the whole scheduler, the same way Type=oneshot with
// Restart=always is under systemd: the command takes one job and exits,
// launchd starts another, and the machine always has exactly one fresh
// runner.
const ephemeralPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xml .Label}}</string>

	<key>ProgramArguments</key>
	<array>
		<string>{{xml .Binary}}</string>
		<string>ephemeral</string>
		<string>run</string>
		<string>--repo</string>
		<string>{{xml .Scope}}</string>
{{- if .ConfigPath}}
		<string>--config</string>
		<string>{{xml .ConfigPath}}</string>
{{- end}}
	</array>

	<!-- One job, then another runner. KeepAlive restarts it however it
	     exited, which is the point: finishing a job successfully is the
	     normal way this command ends. -->
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<!-- A pause, so a runner that cannot register does not spin against
	     GitHub's rate limit. -->
	<key>ThrottleInterval</key>
	<integer>{{.RestartSec}}</integer>

	<key>ExitTimeOut</key>
	<integer>{{.StopTimeout}}</integer>
	<key>ProcessType</key>
	<string>Interactive</string>
	<key>SessionCreate</key>
	<true/>

	<key>StandardOutPath</key>
	<string>{{xml .LogFile}}</string>
	<key>StandardErrorPath</key>
	<string>{{xml .LogFile}}</string>

	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>{{xml .PATH}}</string>
	</dict>
</dict>
</plist>
`

type launchdValues struct {
	Label       string
	Runner      string
	Scope       string
	Binary      string
	ConfigPath  string
	Dir         string
	LogFile     string
	PATH        string
	RestartSec  int
	StopTimeout int
}

// renderPlist fills a template, escaping every value that goes into it.
//
// Paths come from a configuration file and a home directory, and "&" is a
// legal character in both. An unescaped one produces a plist that launchd
// refuses to load, with an error naming a line number rather than a cause.
func renderPlist(text string, values launchdValues) (string, error) {
	tmpl, err := template.New("plist").Funcs(template.FuncMap{"xml": xmlEscape}).Parse(text)
	if err != nil {
		return "", fmt.Errorf("build the plist template: %w", err)
	}

	var rendered strings.Builder
	if err := tmpl.Execute(&rendered, values); err != nil {
		return "", fmt.Errorf("render the plist: %w", err)
	}
	return rendered.String(), nil
}

func xmlEscape(s string) string {
	var out strings.Builder
	if err := xml.EscapeText(&out, []byte(s)); err != nil {
		// EscapeText only fails if the writer does, and this one cannot.
		return s
	}
	return out.String()
}

// launchdLogFile is where a job's output goes, since macOS has no journal.
func launchdLogFile(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "runnerly-"+name+".log")
	}
	return filepath.Join(home, "Library", "Logs", "runnerly", name+".log")
}

func newAgentLaunchdCommand(e *env) *cobra.Command {
	var (
		binary      string
		output      string
		logFile     string
		path        string
		stopTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "launchd [name]",
		Short: "Print a launchd property list for supervising a runner on macOS",
		Long: "launchd prints the macOS equivalent of `agent systemd`: a LaunchAgent that\n" +
			"starts runnerly-agent at login and restarts it if it fails.\n\n" +
			"It writes nothing outside the path you give it and loads nothing. Read the\n" +
			"plist, then install it with the commands printed alongside.\n\n" +
			"A LaunchAgent, not a LaunchDaemon: a daemon starts without a login session,\n" +
			"and a Mac build usually needs one for the keychain, code signing and\n" +
			"anything with a window server. The cost is that the machine has to log in.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			installed, err := e.resolveInstalledRunner(args)
			if err != nil {
				return err
			}

			if binary == "" {
				binary = agentBinaryPath()
			}
			if logFile == "" {
				logFile = launchdLogFile(installed.Name)
			}
			if path == "" {
				path = launchdPATH
			}

			values := launchdValues{
				Label:       launchdDomain + ".agent." + installed.Name,
				Runner:      installed.Name,
				Binary:      binary,
				ConfigPath:  e.configPath,
				Dir:         installed.Dir,
				LogFile:     logFile,
				PATH:        path,
				StopTimeout: int((3 * time.Minute).Seconds()),
			}
			if stopTimeout > 0 {
				values.StopTimeout = int(stopTimeout.Seconds())
			}

			rendered, err := renderPlist(agentPlist, values)
			if err != nil {
				return err
			}
			return e.emitPlist(rendered, output, values,
				"runnerly agent launchd "+installed.Name)
		},
	}

	cmd.Flags().StringVar(&binary, "binary", "", "path to runnerly-agent (default: next to this binary)")
	cmd.Flags().StringVar(&output, "output", "", "write the plist to this file instead of standard output")
	cmd.Flags().StringVar(&logFile, "log-file", "", "where launchd writes the agent's output (default: ~/Library/Logs/runnerly/<name>.log)")
	cmd.Flags().StringVar(&path, "path", "", "PATH the runner inherits (default: Homebrew and the system directories)")
	cmd.Flags().DurationVar(&stopTimeout, "stop-timeout", 0, "how long to let the runner finish its job on stop (default 3m)")
	return cmd
}

func newEphemeralLaunchdCommand(e *env) *cobra.Command {
	var (
		scope      scopeFlags
		binary     string
		output     string
		logFile    string
		path       string
		restartSec int
	)

	cmd := &cobra.Command{
		Use:   "launchd",
		Short: "Print a launchd property list that keeps one fresh runner on a Mac",
		Long: "launchd prints the macOS equivalent of `ephemeral systemd`: a LaunchAgent\n" +
			"that runs the one-job lifecycle in a loop.\n\n" +
			"It installs nothing and loads nothing.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, configPath, _, err := e.loadConfig()
			if err != nil {
				return err
			}
			target, err := e.resolveScope(scope, cfg, "")
			if err != nil {
				return err
			}

			if binary == "" {
				binary = agent.BinaryPath()
			}
			if logFile == "" {
				logFile = filepath.Join(cfg.EphemeralLogDir(configPath), "launchd.log")
			}
			if path == "" {
				path = launchdPATH
			}

			values := launchdValues{
				Label:       launchdDomain + ".ephemeral",
				Scope:       target.String(),
				Binary:      binary,
				ConfigPath:  e.configPath,
				Dir:         cfg.RunnerDir(ephemeralPrefix(cfg, "")),
				LogFile:     logFile,
				PATH:        path,
				RestartSec:  restartSec,
				StopTimeout: int((3 * time.Minute).Seconds()),
			}

			rendered, err := renderPlist(ephemeralPlist, values)
			if err != nil {
				return err
			}
			return e.emitPlist(rendered, output, values, "runnerly ephemeral launchd")
		},
	}

	scope.register(cmd)
	cmd.Flags().StringVar(&binary, "binary", "", "path to runnerly (default: this binary)")
	cmd.Flags().StringVar(&output, "output", "", "write the plist to this file instead of standard output")
	cmd.Flags().StringVar(&logFile, "log-file", "", "where launchd writes the runner's output (default: inside the ephemeral log directory)")
	cmd.Flags().StringVar(&path, "path", "", "PATH the runner inherits (default: Homebrew and the system directories)")
	cmd.Flags().IntVar(&restartSec, "restart-seconds", 5, "pause between runs, so a failing runner does not spin")
	return cmd
}

// emitPlist writes or prints the plist and then says how to install it.
func (e *env) emitPlist(rendered, output string, v launchdValues, regenerate string) error {
	p := e.printer()
	file := v.Label + ".plist"

	if output != "" {
		if err := os.WriteFile(output, []byte(rendered), 0o644); err != nil { //nolint:gosec // a plist is world readable by design
			return fmt.Errorf("write %s: %w", output, err)
		}
		p.Pass("wrote %s", output)
	} else {
		fmt.Fprint(e.out, rendered)
		p.Println()
	}

	printLaunchdInstructions(p, file, v, output, regenerate)
	return nil
}

func printLaunchdInstructions(p *ui.Printer, file string, v launchdValues, writtenTo, regenerate string) {
	p.Dim("# Review the plist, then install it. No root: a LaunchAgent belongs")
	p.Dim("# to the user who runs it.")
	p.Println()

	source := writtenTo
	if source == "" {
		source = file
		p.Println("  " + regenerate + " --output " + source)
	}
	p.Println("  mkdir -p ~/Library/LaunchAgents " + filepath.Dir(v.LogFile))
	p.Println("  cp " + source + " ~/Library/LaunchAgents/" + file)
	p.Println("  launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/" + file)
	p.Println()
	p.Dim("Follow it with: tail -f %s", v.LogFile)
	p.Dim("Stop it with:   launchctl bootout gui/$(id -u)/%s", v.Label)
	p.Println()
	p.Dim("A LaunchAgent runs while the user is logged in. A Mac that should")
	p.Dim("come back after a reboot on its own needs automatic login enabled,")
	p.Dim("in System Settings > Users & Groups > Login Options.")
}
