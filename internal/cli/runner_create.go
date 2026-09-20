package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/runner"
)

func newRunnerCreateCommand(e *env) *cobra.Command {
	var (
		scope       scopeFlags
		name        string
		labels      string
		dir         string
		work        string
		group       string
		ephemeral   bool
		replace     bool
		allowPublic bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Install and register a self-hosted runner on this machine",
		Long: "create installs GitHub's official Actions runner on this machine and\n" +
			"registers it with a repository or organization.\n\n" +
			"It resolves the release GitHub currently expects, verifies the SHA-256\n" +
			"checksum GitHub publishes with it, unpacks it, and runs config.sh with a\n" +
			"short-lived registration token. Runnerly does not store that token.\n\n" +
			"The runner is registered but not started. Supervising the runner process\n" +
			"is the agent's job, which is not implemented yet; start it by hand for now.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, cfg, _, err := e.githubClient()
			if err != nil {
				return err
			}
			target, err := scope.resolve(cfg)
			if err != nil {
				return err
			}

			if name == "" {
				name = defaultRunnerName(cfg)
			}
			if err := validateRunnerName(name); err != nil {
				return err
			}

			if err := e.checkRunnerPolicy(cmd.Context(), client, cfg, target, allowPublic); err != nil {
				return err
			}

			if dir == "" {
				dir = cfg.RunnerDir(name)
			}
			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", dir, err)
			}

			p := e.printer()
			p.Heading("Registering " + name)

			token, err := client.CreateRegistrationToken(cmd.Context(), target)
			if err != nil {
				return err
			}

			result, err := runner.Install(cmd.Context(), e.runnerEnv(), client, target, runner.Options{
				Dir:               absDir,
				Name:              name,
				Labels:            splitLabels(labels, cfg),
				URL:               target.WebURL(cfg.GitHub.Host),
				RegistrationToken: token.Token,
				Group:             group,
				Work:              work,
				Ephemeral:         ephemeral,
				Replace:           replace,
				Progress:          func(step string) { p.Dim("  %s", step) },
			})
			if err != nil {
				return err
			}

			p.Println()
			p.Pass("%s is registered with %s", name, target)
			p.Println()
			p.Println("  scope   ", target.String())
			p.Println("  labels  ", strings.Join(result.Labels, ","))
			p.Println("  runner  ", result.Filename)
			p.Println("  dir     ", result.Dir)
			p.Println()
			p.Println("Start it:")
			p.Println("  cd " + result.Dir + " && ./run.sh")
			p.Println()
			p.Dim("Runnerly does not supervise the runner process yet. Until the agent ships,")
			p.Dim("run it yourself or install a service for it.")
			return nil
		},
	}

	scope.register(cmd)
	cmd.Flags().StringVar(&name, "name", "", "runner name in GitHub (default: runner.name, else this machine's hostname)")
	cmd.Flags().StringVar(&labels, "labels", "", "comma-separated custom labels (default: runner.labels from the configuration)")
	cmd.Flags().StringVar(&dir, "dir", "", "where to install the runner (default: runner.dir plus the runner name)")
	cmd.Flags().StringVar(&work, "work", "_work", "the runner's working directory, relative to its install directory")
	cmd.Flags().StringVar(&group, "group", "", "runner group to join (organization runners)")
	cmd.Flags().BoolVar(&ephemeral, "ephemeral", false, "accept one job, then deregister")
	cmd.Flags().BoolVar(&replace, "replace", false, "take over an existing registration with the same name")
	cmd.Flags().BoolVar(&allowPublic, "allow-public", false,
		"allow registering against a public repository (see docs/security.md)")
	return cmd
}

// checkRunnerPolicy enforces the security policy before anything is installed.
//
// A self-hosted runner on a public repository will execute code from anyone who
// can get a workflow to run, so Runnerly refuses by default rather than leaving
// this to documentation.
func (e *env) checkRunnerPolicy(ctx context.Context, client *github.Client, cfg config.Config,
	scope github.Scope, allowPublic bool,
) error {
	if cfg.Security.AllowPublicRepositories || allowPublic {
		return nil
	}
	if scope.Kind != github.KindRepository {
		// An organization's repositories cannot be enumerated cheaply, and a
		// runner group may already restrict which of them may use it.
		e.printer().Warn("organization runners are not checked against the public-repository policy")
		return nil
	}

	repo, err := client.Repository(ctx, scope)
	if err != nil {
		return err
	}
	if repo.Private {
		return nil
	}

	return fmt.Errorf("%s is a public repository, and security.allow_public_repositories is false.\n\n"+
		"A self-hosted runner executes the code in a workflow. On a public repository,\n"+
		"anyone who can get a workflow to run can run commands on this machine.\n"+
		"See docs/security.md.\n\n"+
		"If you have read that and still want this, pass --allow-public or set\n"+
		"security.allow_public_repositories: true in the configuration", scope)
}

// runnerNamePattern matches what GitHub accepts and what is safe as a
// directory name.
var runnerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateRunnerName(name string) error {
	if !runnerNamePattern.MatchString(name) {
		return fmt.Errorf("%q is not a usable runner name.\n"+
			"Use letters, digits, '.', '_' and '-', starting with a letter or digit", name)
	}
	return nil
}

// defaultRunnerName prefers the configured name, then this machine's hostname,
// so two machines do not collide by default.
func defaultRunnerName(cfg config.Config) string {
	if cfg.Runner.Name != "" {
		return cfg.Runner.Name
	}
	host, err := os.Hostname()
	if err != nil {
		return "runnerly-01"
	}
	// A hostname may carry characters GitHub will not take.
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, host)
	cleaned = strings.Trim(cleaned, "-._")
	if cleaned == "" {
		return "runnerly-01"
	}
	return cleaned
}

// splitLabels parses the --labels flag, falling back to the configuration.
func splitLabels(flag string, cfg config.Config) []string {
	if strings.TrimSpace(flag) == "" {
		return cfg.Runner.Labels
	}
	var out []string
	for _, l := range strings.Split(flag, ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
