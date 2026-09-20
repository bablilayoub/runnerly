package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/auth"
	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/controlplane"
	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/jobstate"
	"github.com/bablilayoub/runnerly/internal/runner"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/ui"
)

func newEphemeralCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ephemeral",
		Short: "Run one-job runners",
		Long: "ephemeral runs a runner that accepts a single job and is then taken\n" +
			"apart again.\n\n" +
			"Each job gets a registration of its own and a fresh working directory,\n" +
			"which is what GitHub recommends when a job must not inherit anything from\n" +
			"the one before it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newEphemeralRunCommand(e), newEphemeralLogsCommand(e), newEphemeralSystemdCommand(e))
	return cmd
}

// ephemeralUnit is the service file `ephemeral systemd` emits.
//
// Type=oneshot with Restart=always is the whole scheduler: the command runs
// one job and exits, systemd starts it again, and the machine always has
// exactly one fresh runner. Deciding how many machines to have is
// autoscaling, which Runnerly does not do.
const ephemeralUnit = `[Unit]
Description=Runnerly ephemeral runner for {{.Scope}}
Documentation=https://github.com/bablilayoub/runnerly
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User={{.User}}
Group={{.Group}}
ExecStart={{.Binary}} ephemeral run --repo {{.Scope}}{{if .ConfigPath}} --config {{.ConfigPath}}{{end}}

# One job, then start another runner. This is the loop: the command returns
# when its job is done, and systemd brings up the next one.
Restart=always
# A pause, so a runner that cannot register does not spin against GitHub's
# rate limit.
RestartSec={{.RestartSec}}

# The runner finishes the job it is on when asked to stop.
KillSignal=SIGTERM
TimeoutStopSec={{.StopTimeout}}

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only
ReadWritePaths={{.Dir}} {{.LogDir}}

[Install]
WantedBy=multi-user.target
`

type ephemeralUnitValues struct {
	Scope       string
	User        string
	Group       string
	Binary      string
	ConfigPath  string
	Dir         string
	LogDir      string
	RestartSec  int
	StopTimeout int
}

func newEphemeralSystemdCommand(e *env) *cobra.Command {
	var (
		scope      scopeFlags
		unitUser   string
		unitGroup  string
		binary     string
		output     string
		restartSec int
	)

	cmd := &cobra.Command{
		Use:   "systemd",
		Short: "Print a systemd unit that keeps one fresh runner on this machine",
		Long: "systemd prints a service file that runs the one-job lifecycle in a loop.\n\n" +
			"It installs nothing: writing to /etc and enabling a service needs root, and\n" +
			"Runnerly does not take privileges it was not handed.",
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
			if unitGroup == "" {
				unitGroup = unitUser
			}

			values := ephemeralUnitValues{
				Scope:       target.String(),
				User:        unitUser,
				Group:       unitGroup,
				Binary:      binary,
				ConfigPath:  e.configPath,
				Dir:         cfg.RunnerDir(ephemeralPrefix(cfg, "")),
				LogDir:      cfg.EphemeralLogDir(configPath),
				RestartSec:  restartSec,
				StopTimeout: int((3 * time.Minute).Seconds()),
			}

			tmpl, err := template.New("unit").Parse(ephemeralUnit)
			if err != nil {
				return fmt.Errorf("build the unit template: %w", err)
			}
			var rendered strings.Builder
			if err := tmpl.Execute(&rendered, values); err != nil {
				return fmt.Errorf("render the unit: %w", err)
			}

			unitName := "runnerly-ephemeral.service"
			p := e.printer()

			if output != "" {
				if err := os.WriteFile(output, []byte(rendered.String()), 0o644); err != nil { //nolint:gosec // a unit file is world readable by design
					return fmt.Errorf("write %s: %w", output, err)
				}
				p.Pass("wrote %s", output)
			} else {
				fmt.Fprint(e.out, rendered.String())
				p.Println()
			}

			p.Dim("# Review it, then install. These need root, which is why Runnerly")
			p.Dim("# prints them instead of running them.")
			p.Println()
			source := output
			if source == "" {
				source = unitName
				p.Println("  runnerly ephemeral systemd --output " + source)
			}
			p.Println("  sudo useradd --system --home " + values.Dir + " --shell /usr/sbin/nologin " + values.User)
			p.Println("  sudo mkdir -p " + values.Dir + " " + values.LogDir)
			p.Println("  sudo chown -R " + values.User + ":" + values.Group + " " + values.Dir + " " + values.LogDir)
			p.Println("  sudo install -m 0644 " + source + " /etc/systemd/system/" + unitName)
			p.Println("  sudo systemctl daemon-reload")
			p.Println("  sudo systemctl enable --now " + unitName)
			p.Println()
			p.Dim("Each run takes one job and exits; systemd starts the next.")
			return nil
		},
	}

	scope.register(cmd)
	cmd.Flags().StringVar(&unitUser, "user", "runnerly", "user the runner runs as")
	cmd.Flags().StringVar(&unitGroup, "group", "", "group (default: the same as --user)")
	cmd.Flags().StringVar(&binary, "binary", "", "path to runnerly (default: this binary)")
	cmd.Flags().StringVar(&output, "output", "", "write the unit to this file instead of standard output")
	cmd.Flags().IntVar(&restartSec, "restart-seconds", 5, "pause between runs, so a failing runner does not spin")
	return cmd
}

func newEphemeralRunCommand(e *env) *cobra.Command {
	var (
		scope       scopeFlags
		prefix      string
		labels      string
		dir         string
		purge       bool
		allowPublic bool
		logFormat   string
		logLevel    string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Register a runner, let it take one job, then take it apart",
		Long: "run performs the whole one-job lifecycle:\n\n" +
			"  register -> online -> one job -> cleanup -> deregister -> destroy\n\n" +
			"It returns when the job is done. Nothing schedules the next one: pair it\n" +
			"with a service manager set to restart it, and the machine always has one\n" +
			"fresh runner. Provisioning machines to meet demand is autoscaling, which\n" +
			"Runnerly does not do.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, configPath, _, err := e.loadConfig()
			if err != nil {
				return err
			}
			client, _, _, err := e.githubClient()
			if err != nil {
				return err
			}
			target, err := e.resolveScope(scope, cfg, "")
			if err != nil {
				return err
			}

			name, err := ephemeralName(cfg, prefix)
			if err != nil {
				return err
			}
			if err := e.checkRunnerPolicy(cmd.Context(), client, cfg, target, allowPublic); err != nil {
				return err
			}

			if dir == "" {
				// One directory per prefix, reused across runs. The clean
				// environment comes from a fresh registration and a fresh
				// working directory, not from unpacking the same immutable
				// tarball again; re-downloading it every job would make each
				// one slower for nothing.
				dir = cfg.RunnerDir(ephemeralPrefix(cfg, prefix))
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", dir, err)
			}

			p := e.printer()
			p.Heading("Ephemeral runner " + name)

			installed, err := e.registerEphemeral(cmd.Context(), client, cfg, target, name, absDir, labels)
			if err != nil {
				return err
			}

			// From here on the runner exists in GitHub, so every path must
			// end in teardown.
			outcome, plane, runErr := e.superviseEphemeral(cmd.Context(), cfg, configPath, installed, logFormat, logLevel)
			teardown := e.teardownEphemeral(cmd.Context(), client, plane, cfg, configPath, target, installed, outcome, purge)

			p.Println()
			switch {
			case runErr != nil:
				return runErr
			case outcome.Completed:
				p.Pass("%s ran %s in %s", name, outcome.Job.Describe(),
					outcome.Job.Duration().Round(time.Second))
			default:
				p.Warn("%s stopped without running a job", name)
			}
			for _, line := range teardown {
				p.Detail(line)
			}
			return nil
		},
	}

	scope.register(cmd)
	cmd.Flags().StringVar(&prefix, "name-prefix", "", "start of the generated runner name (default: ephemeral.name_prefix, else the hostname)")
	cmd.Flags().StringVar(&labels, "labels", "", "comma-separated custom labels (default: runner.labels)")
	cmd.Flags().StringVar(&dir, "dir", "", "where to install the runner (default: runner.dir plus the name prefix)")
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the unpacked runner, so the next run downloads it again")
	cmd.Flags().BoolVar(&allowPublic, "allow-public", false, "allow registering against a public repository (see docs/security.md)")
	cmd.Flags().StringVar(&logFormat, "log-format", "text", "agent log format: text or json")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "agent log level")
	return cmd
}

// registerEphemeral installs and registers a one-job runner.
func (e *env) registerEphemeral(ctx context.Context, client *github.Client, cfg config.Config,
	target github.Scope, name, dir, labels string,
) (state.Runner, error) {
	p := e.printer()

	token, err := client.CreateRegistrationToken(ctx, target)
	if err != nil {
		return state.Runner{}, err
	}

	result, err := runner.Install(ctx, e.runnerEnv(), client, target, runner.Options{
		Dir:               dir,
		Name:              name,
		Labels:            splitLabels(labels, cfg),
		URL:               target.WebURL(cfg.GitHub.Host),
		RegistrationToken: token.Token,
		Ephemeral:         true,
		// Always replace: the directory is reused between runs, and a
		// previous run that died leaves its registration behind.
		Replace:  true,
		Progress: func(step string) { p.Dim("  %s", step) },
	})
	if err != nil {
		return state.Runner{}, err
	}

	installed := state.Runner{
		Name:      name,
		Scope:     target,
		Host:      cfg.GitHub.Host,
		Dir:       result.Dir,
		Labels:    result.Labels,
		Ephemeral: true,
		Release:   result.Filename,
	}
	if err := state.Put(e.statePath(), installed); err != nil {
		p.Warn("could not record the runner on this machine: %v", err)
	}
	return installed, nil
}

// superviseEphemeral runs the runner for one job, keeping its output.
func (e *env) superviseEphemeral(ctx context.Context, cfg config.Config, configPath string,
	installed state.Runner, logFormat, logLevel string,
) (agent.Outcome, *controlplane.Client, error) {
	p := e.printer()

	logger, err := agent.NewLogger(e.out, agent.LogFormat(logFormat), logLevel)
	if err != nil {
		return agent.Outcome{}, nil, err
	}

	// The runner's own output goes to a file outside its directory, because
	// the directory is about to be taken apart and the log is the only
	// record of what the job did.
	logPath, logFile, err := openRunLog(cfg, configPath, installed.Name)
	if err != nil {
		p.Warn("could not open a log file, so this run's output will not be kept: %v", err)
	}
	output := e.errOut
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
		output = io.MultiWriter(e.errOut, logFile)
		p.Dim("  logging to %s", logPath)
	}

	opts := agent.Options{
		Runner: installed,
		Logger: logger,
		Output: output,
	}
	opts.Hooks, opts.ExtraEnv = agent.ConfigureEphemeral(cfg, configPath)

	if serverURL := agent.ServerURLFrom(cfg.Server.URL); serverURL != "" {
		enrollment, err := agent.Enroll(ctx, agent.EnrollOptions{
			ServerURL:       serverURL,
			EnrollmentToken: agent.EnrollmentTokenFrom(cfg.Agent.EnrollmentToken),
			CredentialsPath: auth.Path(configPath),
			Runner:          installed,
			Logger:          logger,
		})
		if err != nil {
			// Reporting is a convenience. Refusing to run the job because
			// the control plane is unreachable would be worse than running
			// it unobserved.
			p.Warn("could not reach the control plane, so this run is not reported: %v", err)
		} else if enrollment != nil {
			opts.ControlPlane = enrollment.Client
			opts.HeartbeatInterval = enrollment.Interval
			opts.OnMachineToken = agent.PersistMachineToken(
				auth.Path(configPath), serverURL, enrollment.RunnerID)
		}
	}

	if e.runAgent != nil {
		outcome, err := e.runAgent(ctx, opts)
		return outcome, opts.ControlPlane, err
	}
	outcome, err := agent.Run(ctx, opts)
	return outcome, opts.ControlPlane, err
}

// teardownEphemeral performs the cleanup, deregister and destroy steps, and
// returns what it did.
//
// Every step is attempted even if an earlier one failed: a runner left half
// torn down is worse than one that reports what it could not finish.
func (e *env) teardownEphemeral(ctx context.Context, client *github.Client, plane *controlplane.Client,
	cfg config.Config, configPath string, target github.Scope, installed state.Runner,
	outcome agent.Outcome, purge bool,
) []string {
	var done []string

	// Tell the control plane first, while the credential still works. A
	// retired runner reads as finished on the dashboard instead of as one
	// that stopped answering.
	if plane != nil {
		reason := "stopped without running a job"
		if outcome.Completed {
			reason = "ran " + outcome.Job.Describe()
		}
		if err := plane.Retire(ctx, reason); err != nil {
			done = append(done, "could not tell the control plane it retired: "+err.Error())
		} else {
			done = append(done, "reported as retired to the control plane")
		}
	}

	// Deregister. GitHub removes an ephemeral runner itself once it has run
	// a job, so this usually finds nothing — which is the point: a runner
	// that stopped *without* running one would otherwise linger.
	switch found, err := client.FindRunnerByName(ctx, target, installed.Name); {
	case err == nil:
		if err := client.DeleteRunner(ctx, target, found.ID); err != nil {
			done = append(done, "could not deregister from GitHub: "+err.Error())
		} else {
			done = append(done, "deregistered from "+target.String())
		}
	case errors.Is(err, github.ErrRunnerNotFound):
		done = append(done, "GitHub had already removed the registration")
	default:
		done = append(done, "could not check the registration: "+err.Error())
	}

	// Destroy. The runner's credentials and working directory go; the
	// unpacked binaries stay unless asked, so the next run does not
	// re-download a few hundred megabytes of the same immutable archive.
	if purge {
		if err := os.RemoveAll(installed.Dir); err != nil {
			done = append(done, "could not delete "+installed.Dir+": "+err.Error())
		} else {
			done = append(done, "deleted "+installed.Dir)
		}
	} else if removed, err := destroyRunnerState(installed.Dir); err != nil {
		done = append(done, "could not clear the runner's state: "+err.Error())
	} else if removed > 0 {
		done = append(done, fmt.Sprintf("cleared the runner's credentials and working directory (%d items)", removed))
	}

	if forgotten, err := state.Delete(e.statePath(), installed.Name); err != nil {
		done = append(done, "could not update "+e.statePath()+": "+err.Error())
	} else if forgotten {
		done = append(done, "forgot it on this machine")
	}

	if kept := cfg.Ephemeral.KeepRuns; kept > 0 {
		if pruned, err := pruneRunLogs(cfg.EphemeralLogDir(configPath), kept); err != nil {
			done = append(done, "could not prune old logs: "+err.Error())
		} else if pruned > 0 {
			done = append(done, fmt.Sprintf("removed %d old log(s)", pruned))
		}
	}

	return done
}

// ephemeralLeftovers are the files that make a runner directory "registered".
// Removing them is what makes the next run a genuinely fresh registration.
var ephemeralLeftovers = []string{
	".runner",
	".credentials",
	".credentials_rsa",
	"_work",
	jobstate.Dir,
}

// removeUnpackedRunner deletes the runner binaries so the next install
// fetches the current release. It keeps the directory itself.
func removeUnpackedRunner(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// destroyRunnerState removes a runner's identity and working directory,
// leaving the unpacked binaries in place.
func destroyRunnerState(dir string) (int, error) {
	var removed int
	var problems []string

	for _, name := range ephemeralLeftovers {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		removed++
	}
	if len(problems) > 0 {
		return removed, errors.New(strings.Join(problems, "; "))
	}
	return removed, nil
}

// openRunLog creates the file this run's output is kept in.
func openRunLog(cfg config.Config, configPath, name string) (string, *os.File, error) {
	dir := cfg.EphemeralLogDir(configPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", nil, fmt.Errorf("create %s: %w", dir, err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%s-%s.log", time.Now().UTC().Format("20060102-150405"), name))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // inside the operator's own log directory
	if err != nil {
		return "", nil, fmt.Errorf("create %s: %w", path, err)
	}
	return path, file, nil
}

// pruneRunLogs keeps the newest logs and removes the rest.
func pruneRunLogs(dir string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var logs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
			logs = append(logs, entry.Name())
		}
	}
	if len(logs) <= keep {
		return 0, nil
	}

	// Names start with a UTC timestamp, so sorting them sorts by age.
	sort.Strings(logs)

	var removed int
	for _, name := range logs[:len(logs)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			removed++
		}
	}
	return removed, nil
}

// ephemeralPrefix is the stable part of a generated name.
func ephemeralPrefix(cfg config.Config, flag string) string {
	switch {
	case flag != "":
		return flag
	case cfg.Ephemeral.NamePrefix != "":
		return cfg.Ephemeral.NamePrefix
	default:
		return defaultRunnerName(cfg)
	}
}

// ephemeralName generates a name unique to this run.
//
// Every run registers separately, so reusing a name would collide with a
// registration GitHub has not finished removing.
func ephemeralName(cfg config.Config, flag string) (string, error) {
	prefix := ephemeralPrefix(cfg, flag)
	if err := validateRunnerName(prefix); err != nil {
		return "", err
	}

	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate a runner name: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(b[:]), nil
}

func newEphemeralLogsCommand(e *env) *cobra.Command {
	var follow int

	cmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "List the logs kept from destroyed runners",
		Long: "logs shows what past ephemeral runs left behind.\n\n" +
			"Their output is kept outside the runner directory on purpose: the runner\n" +
			"is taken apart when its job finishes, and without this there would be\n" +
			"nothing left to debug a failure with.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, configPath, _, err := e.loadConfig()
			if err != nil {
				return err
			}
			dir := cfg.EphemeralLogDir(configPath)

			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					p := e.printer()
					p.Skip("no ephemeral runs have been logged")
					p.Detail("Logs appear in " + dir + " once `runnerly ephemeral run` has been used.")
					return nil
				}
				return fmt.Errorf("read %s: %w", dir, err)
			}

			var logs []os.DirEntry
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
					continue
				}
				if len(args) == 1 && !strings.Contains(entry.Name(), args[0]) {
					continue
				}
				logs = append(logs, entry)
			}

			p := e.printer()
			if len(logs) == 0 {
				p.Skip("no logs matched")
				return nil
			}

			sort.Slice(logs, func(i, j int) bool { return logs[i].Name() > logs[j].Name() })
			if follow > 0 && len(logs) > follow {
				logs = logs[:follow]
			}

			table := ui.NewTable("LOG", "SIZE", "MODIFIED")
			for _, entry := range logs {
				info, err := entry.Info()
				if err != nil {
					continue
				}
				table.Row(entry.Name(), humanSize(info.Size()), info.ModTime().Format(time.RFC3339))
			}
			p.Table(table)
			p.Println()
			p.Dim("Read one with: cat %s/<name>", dir)
			return nil
		},
	}

	cmd.Flags().IntVar(&follow, "limit", 20, "how many to show (0 for all)")
	return cmd
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMG"[exp])
}
