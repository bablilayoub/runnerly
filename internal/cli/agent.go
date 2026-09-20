package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/auth"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/ui"
)

func newAgentCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run and supervise a runner on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newAgentRunCommand(e),
		newAgentStatusCommand(e),
		newAgentSystemdCommand(e),
		newHookCommand(e),
	)
	return cmd
}

// resolveInstalledRunner finds the runner a command acts on: the one named, or
// the only one installed here.
func (e *env) resolveInstalledRunner(args []string) (state.Runner, error) {
	if len(args) == 1 {
		runner, err := state.Get(e.statePath(), args[0])
		if errors.Is(err, state.ErrNotFound) {
			return state.Runner{}, fmt.Errorf("%w.\n"+
				"Installed runners are recorded in %s; run `runnerly agent status` to list them",
				err, e.statePath())
		}
		return runner, err
	}
	return state.Only(e.statePath())
}

func newAgentRunCommand(e *env) *cobra.Command {
	var (
		logFormat   string
		logLevel    string
		quietRunner bool
	)

	cmd := &cobra.Command{
		Use:   "run [name]",
		Short: "Supervise a runner in the foreground",
		Long: "run starts the official GitHub runner and keeps it running: it restarts the\n" +
			"process when it fails, backing off 5s, 10s, 20s, 40s, 80s, and stops after\n" +
			"that rather than hiding a runner that cannot start.\n\n" +
			"It runs in the foreground and stops cleanly on Ctrl-C or SIGTERM, sending the\n" +
			"runner SIGTERM first so it can finish the job it is on.\n\n" +
			"For a machine that should run a runner at boot, use `runnerly agent systemd`\n" +
			"and let the service manager supervise runnerly-agent instead.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			installed, err := e.resolveInstalledRunner(args)
			if err != nil {
				return err
			}

			logger, err := agent.NewLogger(e.out, agent.LogFormat(logFormat), logLevel)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			opts := agent.Options{Runner: installed, Logger: logger}
			if !quietRunner {
				opts.Output = e.errOut
			}

			cfg, configPath, _, err := e.loadConfig()
			if err != nil {
				return err
			}
			opts.Hooks, opts.ExtraEnv = agent.Configure(cfg, configPath)
			if serverURL := agent.ServerURLFrom(cfg.Server.URL); serverURL != "" {
				enrollment, err := agent.Enroll(ctx, agent.EnrollOptions{
					ServerURL:       serverURL,
					EnrollmentToken: agent.EnrollmentTokenFrom(cfg.Agent.EnrollmentToken),
					CredentialsPath: auth.Path(configPath),
					Runner:          installed,
					Logger:          logger,
				})
				if err != nil {
					return err
				}
				if enrollment != nil {
					opts.ControlPlane = enrollment.Client
					opts.HeartbeatInterval = enrollment.Interval
					opts.OnMachineToken = agent.PersistMachineToken(
						auth.Path(configPath), serverURL, enrollment.RunnerID)
				}
			}

			_, err = agent.Run(ctx, opts)
			return err
		},
	}

	cmd.Flags().StringVar(&logFormat, "log-format", string(agent.FormatText), "log format: text or json")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level: debug, info, warn or error")
	cmd.Flags().BoolVar(&quietRunner, "quiet-runner", false, "discard the runner's own output")
	return cmd
}

func newAgentStatusCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the runners installed on this machine",
		Long: "status reports what Runnerly installed here and whether each one still\n" +
			"looks runnable. It reads only local state and does not contact GitHub, so\n" +
			"it answers a different question from `runnerly runner list`, which reports\n" +
			"what GitHub has registered.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			runners, err := state.List(e.statePath())
			if err != nil {
				return err
			}

			p := e.printer()
			if len(runners) == 0 {
				p.Skip("no runner is installed on this machine")
				p.Detail("Install one with `runnerly runner create --repo owner/repo`.")
				return nil
			}

			table := ui.NewTable("NAME", "SCOPE", "READY", "DIRECTORY")
			var problems []string
			for _, r := range runners {
				ready := "yes"
				if _, err := agent.Preflight(r); err != nil {
					ready = "no"
					problems = append(problems, r.Name+": "+err.Error())
				}
				table.Row(r.Name, r.Scope.String(), ready, r.Dir)
			}
			p.Table(table)

			for _, problem := range problems {
				p.Println()
				p.Fail("%s", problem)
			}
			return nil
		},
	}
	return cmd
}

// systemdUnit is the service file `agent systemd` emits.
//
// The hardening options are the cheap ones that do not interfere with running
// workflow code: a runner legitimately needs to write to its own directory and
// reach the network, so the unit protects the rest of the system instead.
const systemdUnit = `[Unit]
Description=Runnerly agent for the {{.Runner}} GitHub Actions runner
Documentation=https://github.com/bablilayoub/runnerly
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User={{.User}}
Group={{.Group}}
WorkingDirectory={{.Dir}}
ExecStart={{.Binary}} --name {{.Runner}}{{if .ConfigPath}} --config {{.ConfigPath}}{{end}}

# The runner finishes the job it is on when asked to stop, so give it room.
KillSignal=SIGTERM
TimeoutStopSec={{.StopTimeout}}

# systemd restarts the agent itself; the agent restarts the runner. Both are
# needed: the agent handles a crashing runner, this handles a crashing agent.
Restart=always
RestartSec=5

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only
ReadWritePaths={{.Dir}}

[Install]
WantedBy=multi-user.target
`

type systemdValues struct {
	Runner      string
	User        string
	Group       string
	Dir         string
	Binary      string
	ConfigPath  string
	StopTimeout int
}

func newAgentSystemdCommand(e *env) *cobra.Command {
	var (
		unitUser    string
		unitGroup   string
		binary      string
		output      string
		stopTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "systemd [name]",
		Short: "Print a systemd unit for supervising a runner",
		Long: "systemd prints a service file for this machine's runner.\n\n" +
			"It writes nothing outside the path you give it and never installs anything:\n" +
			"putting a file in /etc and enabling a service needs root, and Runnerly does\n" +
			"not take privileges you did not hand it. Read the unit, then install it\n" +
			"yourself with the commands printed alongside.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			installed, err := e.resolveInstalledRunner(args)
			if err != nil {
				return err
			}

			if binary == "" {
				binary = agentBinaryPath()
			}
			if unitGroup == "" {
				unitGroup = unitUser
			}

			values := systemdValues{
				Runner:      installed.Name,
				User:        unitUser,
				Group:       unitGroup,
				Dir:         installed.Dir,
				Binary:      binary,
				ConfigPath:  e.configPath,
				StopTimeout: int((3 * time.Minute).Seconds()),
			}
			if stopTimeout > 0 {
				values.StopTimeout = int(stopTimeout.Seconds())
			}

			tmpl, err := template.New("unit").Parse(systemdUnit)
			if err != nil {
				return fmt.Errorf("build the unit template: %w", err)
			}

			var rendered strings.Builder
			if err := tmpl.Execute(&rendered, values); err != nil {
				return fmt.Errorf("render the unit: %w", err)
			}

			unitName := "runnerly-agent@" + installed.Name + ".service"
			p := e.printer()

			if output != "" {
				if err := os.WriteFile(output, []byte(rendered.String()), 0o644); err != nil { //nolint:gosec // a unit file is world readable by design
					return fmt.Errorf("write %s: %w", output, err)
				}
				p.Pass("wrote %s", output)
				printSystemdInstructions(p, unitName, values, output)
				return nil
			}

			fmt.Fprint(e.out, rendered.String())
			p.Println()
			printSystemdInstructions(p, unitName, values, "")
			return nil
		},
	}

	cmd.Flags().StringVar(&unitUser, "user", "runnerly", "user the agent runs as")
	cmd.Flags().StringVar(&unitGroup, "group", "", "group the agent runs as (default: the same as --user)")
	cmd.Flags().StringVar(&binary, "binary", "", "path to runnerly-agent (default: next to this binary)")
	cmd.Flags().StringVar(&output, "output", "", "write the unit to this file instead of standard output")
	cmd.Flags().DurationVar(&stopTimeout, "stop-timeout", 0, "how long to let the runner finish its job on stop (default 3m)")
	return cmd
}

func printSystemdInstructions(p *ui.Printer, unitName string, v systemdValues, writtenTo string) {
	p.Dim("# Review the unit, then install it. These need root, which is why")
	p.Dim("# Runnerly prints them instead of running them.")
	p.Println()

	source := writtenTo
	if source == "" {
		source = "runnerly-agent.service"
		p.Println("  runnerly agent systemd " + v.Runner + " --output " + source)
	}
	p.Println("  sudo useradd --system --home " + v.Dir + " --shell /usr/sbin/nologin " + v.User)
	p.Println("  sudo chown -R " + v.User + ":" + v.Group + " " + v.Dir)
	p.Println("  sudo install -m 0644 " + source + " /etc/systemd/system/" + unitName)
	p.Println("  sudo systemctl daemon-reload")
	p.Println("  sudo systemctl enable --now " + unitName)
	p.Println()
	p.Dim("Follow it with: journalctl -u %s -f", unitName)
}

// agentBinaryPath guesses where runnerly-agent lives: beside this binary, or
// on PATH.
func agentBinaryPath() string {
	const name = "runnerly-agent"
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(candidate); err == nil {
			if abs, err := filepath.Abs(candidate); err == nil {
				return abs
			}
			return candidate
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return "/usr/local/bin/" + name
}
