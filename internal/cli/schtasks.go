package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode/utf16"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/ui"
)

// Windows has no systemd and no launchd. It has the Service Control
// Manager, which will only start a program written to answer it — and
// runnerly-agent is a console program, so the SCM would start it, wait for
// a reply that never comes, and kill it as unresponsive.
//
// The other way to run something at boot and restart it when it fails is a
// scheduled task, which is what this generates. It needs nothing added to
// the agent and no dependency: the whole configuration is one XML file
// that schtasks imports.
//
// The identity is deliberately not in the file. A task that runs without
// anyone logged in needs an account and a password, and the password goes
// on the command line that imports it, prompted for, rather than into a
// file that would then be sitting on disk holding it.

// taskXML is what `agent schtasks` emits.
//
// The settings that are not defaults, and why:
//
//   - ExecutionTimeLimit PT0S: no limit. The default is three days, after
//     which Windows stops the task — which for a runner means a machine
//     that quietly stops taking jobs every three days.
//   - RestartOnFailure: the agent restarts the runner, this restarts the
//     agent. Both are needed, the same way both are under systemd.
//   - MultipleInstancesPolicy IgnoreNew: a second copy of the agent would
//     fight the first over the same runner directory.
//   - StopIfGoingOnBatteries false: the defaults are written for a laptop
//     doing housekeeping, not for a machine whose job is to be available.
const taskXML = `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>{{xml .Description}}</Description>
    <Author>Runnerly</Author>
    <URI>\{{xml .TaskName}}</URI>
  </RegistrationInfo>
  <Triggers>
    <BootTrigger>
      <Enabled>true</Enabled>
    </BootTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>Password</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>5</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>{{xml .Binary}}</Command>
      <Arguments>{{xml .Arguments}}</Arguments>
      <WorkingDirectory>{{xml .Dir}}</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`

type taskValues struct {
	TaskName    string
	Description string
	Binary      string
	Arguments   string
	Dir         string
}

// utf16LE encodes the document the way schtasks insists on reading it.
//
// This is not a preference. Task Scheduler refuses a UTF-8 document with
// "The task XML is malformed. (1,40)::ERROR: unable to switch the
// encoding", pointing at the encoding declaration and saying nothing
// about what it wanted instead. It was found by importing one.
func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))

	// A byte order mark, then each unit little-endian.
	out := make([]byte, 0, 2+len(units)*2)
	out = append(out, 0xFF, 0xFE)
	for _, u := range units {
		//nolint:gosec // G115: taking the low and high byte of a uint16 is the encoding
		out = append(out, byte(u&0xFF), byte(u>>8))
	}
	return out
}

// renderTask fills the template, escaping every value.
func renderTask(values taskValues) (string, error) {
	tmpl, err := template.New("task").Funcs(template.FuncMap{"xml": xmlEscape}).Parse(taskXML)
	if err != nil {
		return "", fmt.Errorf("build the task template: %w", err)
	}

	var rendered strings.Builder
	if err := tmpl.Execute(&rendered, values); err != nil {
		return "", fmt.Errorf("render the task: %w", err)
	}
	return rendered.String(), nil
}

// quoteArgument wraps a command-line argument for Task Scheduler, which
// hands the Arguments string to CreateProcess as written.
func quoteArgument(s string) string {
	if s == "" {
		return s
	}
	if !strings.ContainsAny(s, " \t") {
		return s
	}
	return `"` + s + `"`
}

func newAgentSchtasksCommand(e *env) *cobra.Command {
	var (
		binary   string
		output   string
		taskName string
	)

	cmd := &cobra.Command{
		Use:   "schtasks [name]",
		Short: "Print a Windows scheduled task for supervising a runner",
		Long: "schtasks prints the Windows equivalent of `agent systemd`: a scheduled\n" +
			"task that starts runnerly-agent at boot and restarts it if it fails.\n\n" +
			"A task rather than a service, because a Windows service has to be written\n" +
			"to answer the Service Control Manager, and runnerly-agent is a console\n" +
			"program. The Service Control Manager would start it, wait for a reply that\n" +
			"never comes, and kill it as unresponsive.\n\n" +
			"It writes nothing outside the path you give it and registers nothing. The\n" +
			"account the task runs as is supplied when you import it, not written into\n" +
			"the file, so no password ends up on disk.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			installed, err := e.resolveInstalledRunner(args)
			if err != nil {
				return err
			}

			if binary == "" {
				binary = agentBinaryPath()
			}
			if taskName == "" {
				taskName = "Runnerly\\runnerly-agent-" + installed.Name
			}

			arguments := "--name " + quoteArgument(installed.Name)
			if e.configPath != "" {
				arguments += " --config " + quoteArgument(e.configPath)
			}

			values := taskValues{
				TaskName:    taskName,
				Description: "Runnerly agent for the " + installed.Name + " GitHub Actions runner",
				Binary:      binary,
				Arguments:   arguments,
				Dir:         installed.Dir,
			}

			rendered, err := renderTask(values)
			if err != nil {
				return err
			}
			return e.emitTask(rendered, output, values,
				"runnerly agent schtasks "+installed.Name)
		},
	}

	cmd.Flags().StringVar(&binary, "binary", "", "path to runnerly-agent.exe (default: next to this binary)")
	cmd.Flags().StringVar(&output, "output", "", "write the task to this file instead of standard output")
	cmd.Flags().StringVar(&taskName, "task-name", "", "name to register the task under (default: Runnerly\\runnerly-agent-<name>)")
	return cmd
}

func newEphemeralSchtasksCommand(e *env) *cobra.Command {
	var (
		scope    scopeFlags
		binary   string
		output   string
		taskName string
	)

	cmd := &cobra.Command{
		Use:   "schtasks",
		Short: "Print a Windows scheduled task that keeps one fresh runner here",
		Long: "schtasks prints the Windows equivalent of `ephemeral systemd`: a task that\n" +
			"runs the one-job lifecycle in a loop.\n\n" +
			"RestartOnFailure is the whole scheduler, the way Restart=always is under\n" +
			"systemd — except that a scheduled task only restarts on a failure, so the\n" +
			"loop runs on its own trigger as well. See the printed commands.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _, _, err := e.loadConfig()
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
			if taskName == "" {
				taskName = "Runnerly\\runnerly-ephemeral"
			}

			arguments := "ephemeral run --repo " + quoteArgument(target.String())
			if e.configPath != "" {
				arguments += " --config " + quoteArgument(e.configPath)
			}

			values := taskValues{
				TaskName:    taskName,
				Description: "Runnerly one-job runner for " + target.String(),
				Binary:      binary,
				Arguments:   arguments,
				Dir:         cfg.RunnerDir(ephemeralPrefix(cfg, "")),
			}

			rendered, err := renderTask(values)
			if err != nil {
				return err
			}
			return e.emitTask(rendered, output, values, "runnerly ephemeral schtasks")
		},
	}

	scope.register(cmd)
	cmd.Flags().StringVar(&binary, "binary", "", "path to runnerly.exe (default: this binary)")
	cmd.Flags().StringVar(&output, "output", "", "write the task to this file instead of standard output")
	cmd.Flags().StringVar(&taskName, "task-name", "", "name to register the task under (default: Runnerly\\runnerly-ephemeral)")
	return cmd
}

// emitTask writes or prints the task and then says how to register it.
func (e *env) emitTask(rendered, output string, v taskValues, regenerate string) error {
	p := e.printer()

	if output != "" {
		if err := os.WriteFile(output, utf16LE(rendered), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", output, err)
		}
		p.Pass("wrote %s", output)
	} else {
		// Readable here, rather than a wall of UTF-16 in a terminal. The
		// file --output writes is the one schtasks will accept, and the
		// note below says so, because redirecting this instead would be
		// rejected for a reason that names a column number.
		fmt.Fprint(e.out, rendered)
		p.Println()
		p.Dim("# schtasks only reads UTF-16. Use --output, which writes it;")
		p.Dim("# redirecting this output produces a file it will refuse.")
		p.Println()
	}

	printSchtasksInstructions(p, v, output, regenerate)
	return nil
}

func printSchtasksInstructions(p *ui.Printer, v taskValues, writtenTo, regenerate string) {
	p.Dim("# Review the task, then register it. Registering one that runs")
	p.Dim("# without a logged-in user needs an administrator.")
	p.Println()

	source := writtenTo
	if source == "" {
		source = "runnerly-agent.xml"
		p.Println("  " + regenerate + " --output " + source)
	}
	p.Println(`  schtasks /create /xml ` + source + ` /tn "` + v.TaskName + `" /ru "%USERDOMAIN%\%USERNAME%" /rp *`)
	p.Println(`  schtasks /run /tn "` + v.TaskName + `"`)
	p.Println()
	p.Dim("/rp * prompts for the password rather than taking it on the command")
	p.Dim("line, which would put it in the console history.")
	p.Println()
	p.Dim(`Check it with:  schtasks /query /tn "%s" /v /fo list`, v.TaskName)
	p.Dim(`Stop it with:   schtasks /end /tn "%s"`, v.TaskName)
	p.Dim(`Remove it with: schtasks /delete /tn "%s" /f`, v.TaskName)
	p.Println()
	p.Dim("There is no journal, so the agent's own output goes nowhere unless")
	p.Dim("you redirect it. Point --log-file at a file, or read the runner's")
	p.Dim("own logs in %s.", filepath.Join(v.Dir, "_diag"))
}
