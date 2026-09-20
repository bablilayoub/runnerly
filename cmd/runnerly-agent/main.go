// Command runnerly-agent supervises the official GitHub Actions runner on one
// machine.
//
// It is the binary a systemd unit runs. Generate a unit with:
//
//	runnerly agent systemd
//
// The agent uses the standard library's flag package rather than the CLI's
// command framework: it is a daemon with a handful of flags, and keeping it
// that way makes it a smaller thing to start at boot.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bablilayoub/runnerly/internal/agent"
	"github.com/bablilayoub/runnerly/internal/auth"
	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath  = flag.String("config", "", "path to config.yaml (default: $RUNNERLY_CONFIG, else the user or system config)")
		name        = flag.String("name", "", "runner to supervise (default: the only runner installed on this machine)")
		logFormat   = flag.String("log-format", string(agent.FormatJSON), "log format: json or text")
		logLevel    = flag.String("log-level", "info", "log level: debug, info, warn or error")
		quietRunner = flag.Bool("quiet-runner", false, "discard the runner's own output instead of forwarding it to stderr")
		showVersion = flag.Bool("version", false, "print build information and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Get().String())
		return 0
	}

	logger, err := agent.NewLogger(os.Stdout, agent.LogFormat(*logFormat), *logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	runner, err := resolveRunner(*configPath, *name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// SIGTERM is what systemd sends on stop, and what the runner needs in
	// order to finish the job it is on rather than abandoning it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The runner's own output is not structured, so it goes to stderr while
	// the agent's structured log goes to stdout. A collector can then read
	// one without having to untangle the other.
	var output *os.File
	if !*quietRunner {
		output = os.Stderr
	}

	opts := agent.Options{Runner: runner, Logger: logger}
	if output != nil {
		opts.Output = output
	}

	resolvedConfig := *configPath
	if resolvedConfig == "" {
		resolvedConfig = config.Path()
	}
	cfg, _, err := config.Load(resolvedConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	opts.Hooks, opts.ExtraEnv = agent.Configure(cfg, resolvedConfig)

	// Reporting is optional: an agent with no control plane configured still
	// supervises its runner and logs what happens.
	enrollment, err := connect(ctx, *configPath, runner, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if enrollment != nil {
		opts.ControlPlane = enrollment.Client
		opts.HeartbeatInterval = enrollment.Interval
		opts.OnMachineToken = agent.PersistMachineToken(
			auth.Path(resolvedConfig), agent.ServerURLFrom(cfg.Server.URL), enrollment.RunnerID)
	}

	if _, err := agent.Run(ctx, opts); err != nil {
		// The failure is already in the structured log; this line is for
		// anyone reading stderr directly.
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// connect enrolls with the control plane when one is configured.
func connect(ctx context.Context, configPath string, runner state.Runner, logger *slog.Logger) (*agent.Enrollment, error) {
	if configPath == "" {
		configPath = config.Path()
	}
	cfg, _, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	serverURL := agent.ServerURLFrom(cfg.Server.URL)
	if serverURL == "" {
		return nil, nil
	}
	return agent.Enroll(ctx, agent.EnrollOptions{
		ServerURL:       serverURL,
		EnrollmentToken: agent.EnrollmentTokenFrom(cfg.Agent.EnrollmentToken),
		CredentialsPath: auth.Path(configPath),
		Runner:          runner,
		Logger:          logger,
	})
}

// resolveRunner finds the runner to supervise, by name or by being the only
// one installed.
func resolveRunner(configPath, name string) (state.Runner, error) {
	if configPath == "" {
		configPath = config.Path()
	}
	statePath := state.Path(configPath)

	if name != "" {
		runner, err := state.Get(statePath, name)
		if errors.Is(err, state.ErrNotFound) {
			return state.Runner{}, fmt.Errorf("%w.\nInstalled runners are recorded in %s", err, statePath)
		}
		return runner, err
	}
	return state.Only(statePath)
}
