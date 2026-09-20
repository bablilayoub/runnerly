// Package cli implements the runnerly command line interface.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/auth"
	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/docker"
	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/runner"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/ui"
	"github.com/bablilayoub/runnerly/internal/version"
)

// ExitError lets a command choose the process exit code without printing an
// extra error line. `runnerly doctor` uses it to exit 1 on a failed check
// after it has already rendered the reason.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// env carries the process-wide state a command needs. It is a struct so tests
// can run commands against buffers instead of the real streams.
type env struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer

	// interactive is true when stdin is a terminal, so a command may prompt.
	interactive bool

	configPath string
	noColor    bool
	token      string

	// newGitHubClient builds the API client. It is a field so tests can point
	// the whole command tree at a fake GitHub without a network.
	newGitHubClient func(host, token string) *github.Client
	// runnerEnv is how the CLI reaches the machine when installing a runner.
	// Tests replace it so no archive is downloaded and no script is executed.
	runnerEnv func() runner.Env
	// newDockerClient builds the Docker client. Nil uses the real one; tests
	// replace it so no daemon is needed.
	newDockerClient func(config.Config) *docker.Client
}

// newEnv returns the default environment, wired to the real GitHub.
func newEnv(in io.Reader, out, errOut io.Writer) *env {
	e := &env{
		in:     in,
		out:    out,
		errOut: errOut,
		newGitHubClient: func(host, token string) *github.Client {
			return github.NewForHost(host, token)
		},
		runnerEnv: runner.DefaultEnv,
	}
	if f, ok := in.(*os.File); ok {
		e.interactive = ui.IsTerminal(f)
	}
	return e
}

// printer returns a Printer for stdout honoring the --no-color flag.
func (e *env) printer() *ui.Printer {
	if e.noColor {
		return ui.NewPlain(e.out)
	}
	return ui.New(e.out)
}

// resolvedConfigPath returns the config file this invocation uses.
func (e *env) resolvedConfigPath() string {
	if e.configPath != "" {
		return e.configPath
	}
	return config.Path()
}

// loadConfig resolves and reads the configuration, reporting which path was
// used so commands can tell the operator.
func (e *env) loadConfig() (cfg config.Config, path string, found bool, err error) {
	path = e.resolvedConfigPath()
	cfg, found, err = config.Load(path)
	return cfg, path, found, err
}

// credentialsPath returns the credentials file beside the configuration.
func (e *env) credentialsPath() string {
	return auth.Path(e.resolvedConfigPath())
}

// githubToken resolves a credential without contacting GitHub.
func (e *env) githubToken(host string) (auth.Token, error) {
	return auth.Resolve(e.credentialsPath(), host, e.token, os.Getenv)
}

// githubClient returns a client for the configured host, and the token it
// used, so commands can tell the operator where the credential came from.
func (e *env) githubClient() (*github.Client, config.Config, auth.Token, error) {
	cfg, _, _, err := e.loadConfig()
	if err != nil {
		return nil, cfg, auth.Token{}, err
	}
	token, err := e.githubToken(cfg.GitHub.Host)
	if err != nil {
		return nil, cfg, auth.Token{}, err
	}
	return e.newGitHubClient(cfg.GitHub.Host, token.Value), cfg, token, nil
}

// scopeFlags are the flags every runner command uses to say where runners
// live. Empty values fall back to the configuration file.
type scopeFlags struct {
	repo string
	org  string
}

func (s *scopeFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.repo, "repo", "", "repository in owner/repo form")
	cmd.Flags().StringVar(&s.org, "org", "", "organization login (runners shared across its repositories)")
	cmd.MarkFlagsMutuallyExclusive("repo", "org")
}

// statePath returns the installed-runner record beside the configuration.
func (e *env) statePath() string {
	return state.Path(e.resolvedConfigPath())
}

// resolveScope decides which repository or organization a command acts on.
//
// Order, most specific first:
//
//  1. an explicit --repo or --org
//  2. the recorded scope of the named runner, if it is installed here
//  3. github.repository or github.organization in the configuration
//  4. the only runner installed on this machine
//
// Steps 2 and 4 exist because a machine that installed a runner already knows
// where it belongs, and asking for --repo again on a single-runner host is
// busywork.
func (e *env) resolveScope(flags scopeFlags, cfg config.Config, runnerName string) (github.Scope, error) {
	switch {
	case flags.repo != "":
		return github.ParseRepository(flags.repo)
	case flags.org != "":
		scope := github.ForOrganization(flags.org)
		return scope, scope.Validate()
	}

	if runnerName != "" {
		if recorded, err := state.Get(e.statePath(), runnerName); err == nil {
			return recorded.Scope, recorded.Scope.Validate()
		}
	}

	switch cfg.GitHub.Scope {
	case config.ScopeOrganization:
		if cfg.GitHub.Organization != "" {
			scope := github.ForOrganization(cfg.GitHub.Organization)
			return scope, scope.Validate()
		}
	default:
		if cfg.GitHub.Repository != "" {
			return github.ParseRepository(cfg.GitHub.Repository)
		}
	}

	// Fall back to the machine's own runner, but only when there is exactly
	// one: guessing between several would act on the wrong repository.
	if only, err := state.Only(e.statePath()); err == nil {
		return only.Scope, only.Scope.Validate()
	}

	if cfg.GitHub.Scope == config.ScopeOrganization {
		return github.Scope{}, errors.New(
			"github.scope is organization but github.organization is empty.\n" +
				"Set it in the configuration, or pass --org")
	}
	return github.Scope{}, errors.New(
		"no repository or organization was given.\n" +
			"Pass --repo owner/repo, or set github.repository in the configuration")
}

// confirm asks the operator to approve an action that cannot be undone from
// the CLI. It refuses rather than assuming yes when it cannot ask.
func confirm(in io.Reader, out io.Writer, interactive bool, prompt string) (bool, error) {
	if !interactive {
		return false, fmt.Errorf("%s\nThis cannot be undone, and there is no terminal to confirm on.\n"+
			"Re-run with --yes if you are sure", prompt)
	}
	fmt.Fprintf(out, "%s [y/N]: ", prompt)

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// NewRootCommand builds the command tree wired to the given streams.
func NewRootCommand(in io.Reader, out, errOut io.Writer) *cobra.Command {
	return newRootCommand(newEnv(in, out, errOut))
}

func newRootCommand(e *env) *cobra.Command {
	root := &cobra.Command{
		Use:   "runnerly",
		Short: "Run GitHub Actions on your own infrastructure",
		Long: "runnerly manages self-hosted GitHub Actions runners.\n\n" +
			"GitHub still schedules and orchestrates workflows. Runnerly manages\n" +
			"the machines that execute them.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Without a subcommand, print help rather than an error.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(e.out)
	root.SetErr(e.errOut)
	root.PersistentFlags().StringVar(&e.configPath, "config", "",
		"path to config.yaml (default: $RUNNERLY_CONFIG, else ~/.config/runnerly/config.yaml)")
	root.PersistentFlags().BoolVar(&e.noColor, "no-color", false, "disable colored output")
	root.PersistentFlags().StringVar(&e.token, "token", "",
		"GitHub token (prefer RUNNERLY_GITHUB_TOKEN; a flag is visible in shell history and process listings)")
	root.SetVersionTemplate("{{.Version}}\n")
	root.Version = version.Get().Short()

	root.AddCommand(
		newVersionCommand(e),
		newDoctorCommand(e),
		newConfigCommand(e),
		newLoginCommand(e),
		newLogoutCommand(e),
		newAuthCommand(e),
		newRepoCommand(e),
		newRunnerCommand(e),
		newAgentCommand(e),
		newServerCommand(e),
	)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := NewRootCommand(os.Stdin, os.Stdout, os.Stderr)
	err := root.Execute()
	if err == nil {
		return 0
	}

	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return 1
}
