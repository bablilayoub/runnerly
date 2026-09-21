package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/auth"
	"github.com/bablilayoub/runnerly/internal/config"
	"github.com/bablilayoub/runnerly/internal/doctor"
	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/runner"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/ui"
)

// maxPickerRepositories is how many repositories the picker shows at once.
// A list longer than a screen is not a picker, it is a wall — so the rest
// are behind a search rather than behind an error.
const maxPickerRepositories = 20

func newSetupCommand(e *env) *cobra.Command {
	var (
		scope       scopeFlags
		name        string
		labels      string
		dir         string
		assumeYes   bool
		allowPublic bool
		skipDoctor  bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set this machine up to run jobs, start to finish",
		Long: "setup does in one command what getting started otherwise takes five:\n" +
			"it checks the machine, makes sure there is a GitHub token, writes a\n" +
			"configuration if there is none, registers a runner, and prints the\n" +
			"commands that keep it running.\n\n" +
			"Nothing is changed until it has shown you what it is going to do. Every\n" +
			"step it takes has a command of its own, so nothing here is a black box:\n" +
			"doctor, login, config init, runner create, agent systemd.\n\n" +
			"For provisioning, pass --repo and --yes and supply the token through\n" +
			"RUNNERLY_GITHUB_TOKEN. It then prompts for nothing and fails rather than\n" +
			"asking.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := &setup{
				env:         e,
				p:           e.printer(),
				scope:       scope,
				name:        name,
				labels:      labels,
				dir:         dir,
				assumeYes:   assumeYes,
				allowPublic: allowPublic,
				skipDoctor:  skipDoctor,
			}
			return s.run(cmd.Context())
		},
	}

	scope.register(cmd)
	cmd.Flags().StringVar(&name, "name", "", "runner name in GitHub (default: runner.name, else the hostname)")
	cmd.Flags().StringVar(&labels, "labels", "", "comma-separated custom labels (default: runner.labels)")
	cmd.Flags().StringVar(&dir, "dir", "", "install location (default: runner.dir plus the runner name)")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "do not ask anything; fail instead of prompting")
	cmd.Flags().BoolVar(&allowPublic, "allow-public", false, "override the public-repository policy for this run")
	cmd.Flags().BoolVar(&skipDoctor, "skip-doctor", false, "do not check the machine first")
	return cmd
}

// setup carries one run's state so the steps read as steps rather than as one
// long function.
type setup struct {
	env *env
	p   *ui.Printer

	scope       scopeFlags
	name        string
	labels      string
	dir         string
	assumeYes   bool
	allowPublic bool
	skipDoctor  bool

	cfg        config.Config
	configPath string
	// in is the buffered reader every question shares, so one prompt does
	// not swallow the answer to the next.
	in     *bufio.Reader
	target github.Scope
	client *github.Client
}

func (s *setup) run(ctx context.Context) error {
	cfg, path, found, err := s.env.loadConfig()
	if err != nil {
		return err
	}
	s.cfg, s.configPath = cfg, path

	s.p.Heading("Setting up Runnerly")

	if err := s.checkMachine(ctx); err != nil {
		return err
	}
	if err := s.ensureToken(ctx); err != nil {
		return err
	}
	if err := s.chooseScope(ctx); err != nil {
		return err
	}
	if s.name == "" {
		s.name = defaultRunnerName(s.cfg)
	}
	if err := validateRunnerName(s.name); err != nil {
		return err
	}
	if err := s.env.checkRunnerPolicy(ctx, s.client, s.cfg, s.target, s.allowPublic); err != nil {
		return err
	}

	proceed, err := s.confirmPlan(found)
	if err != nil {
		return err
	}
	if !proceed {
		s.p.Println()
		s.p.Println("Nothing was changed.")
		return nil
	}

	if !found {
		if err := s.writeConfig(); err != nil {
			return err
		}
	}
	if err := s.registerRunner(ctx); err != nil {
		return err
	}

	s.finish()
	return nil
}

// checkMachine refuses to continue past a failing check. Registering a runner
// on a machine that cannot run one produces a runner that looks fine in
// GitHub and never picks up a job.
func (s *setup) checkMachine(ctx context.Context) error {
	if s.skipDoctor {
		s.p.Skip("machine check skipped")
		s.p.Println()
		return nil
	}

	token, _ := s.env.githubToken(s.cfg.GitHub.Host)
	report := doctor.Run(ctx, doctor.Options{
		Config:      s.cfg,
		ConfigPath:  s.configPath,
		ConfigFound: s.configPath != "" && fileExists(s.configPath),
		Token:       token.Value,
		TokenOrigin: token.Origin,
		Env:         doctor.DefaultEnv(),
	})

	if report.OK() {
		warnings := 0
		for _, c := range report.Checks {
			if c.Status == doctor.StatusWarn {
				warnings++
			}
		}
		switch warnings {
		case 0:
			s.p.Pass("the machine is ready")
		case 1:
			s.p.Pass("the machine is ready, with 1 warning")
		default:
			s.p.Pass("the machine is ready, with %d warnings", warnings)
		}
		if warnings > 0 {
			s.p.Detail("Run `runnerly doctor` to read them.")
		}
		s.p.Println()
		return nil
	}

	// Print the failures only. The whole report is one command away, and
	// burying three failures in twenty passing lines helps nobody.
	s.p.Fail("this machine is not ready")
	s.p.Println()
	for _, c := range report.Checks {
		if c.Status != doctor.StatusFail {
			continue
		}
		s.p.Fail("%s", c.Name)
		if c.Detail != "" {
			s.p.Detail(c.Detail)
		}
		if c.Remedy != "" {
			s.p.Remedy(c.Remedy)
		}
	}
	s.p.Println()
	return errors.New("fix the checks above and run `runnerly setup` again.\n" +
		"`runnerly doctor` shows the whole report, and --skip-doctor goes ahead anyway")
}

// ensureToken resolves a credential, prompting for one only when it has a
// terminal to prompt on and nothing else supplied it.
func (s *setup) ensureToken(ctx context.Context) error {
	host := s.cfg.GitHub.Host
	if token, err := s.env.githubToken(host); err == nil && token.Value != "" {
		client := s.env.newGitHubClient(host, token.Value)
		identity, err := client.Identify(ctx)
		if err != nil {
			return fmt.Errorf("the stored token was not accepted by %s.\n"+
				"Replace it with `runnerly login --with-token`, or unset %s.\n%w",
				host, auth.EnvVarNames()[0], err)
		}
		s.client = client
		s.p.Pass("signed in to %s as %s", host, identity.User.Login)
		s.p.Detail("Token from " + token.Origin + ".")
		s.p.Println()
		return nil
	}

	if s.assumeYes {
		return fmt.Errorf("no GitHub token was found, and --yes means not asking for one.\n"+
			"Set %s, or run: echo \"$GITHUB_TOKEN\" | runnerly login --with-token",
			auth.EnvVarNames()[0])
	}

	s.p.Println("Runnerly needs a GitHub token to register a runner.")
	s.p.Detail(fmt.Sprintf("Create one at https://%s/settings/tokens with the `repo` scope\n"+
		"(or `admin:org` for an organization runner). It is not echoed.", host))
	s.p.Println()

	token, err := ui.ReadSecret(s.env.in, s.env.out, "  Token: ")
	if err != nil {
		if errors.Is(err, ui.ErrNoSecretPrompt) {
			return fmt.Errorf("there is no terminal to type a token into without it being echoed.\n" +
				"Pipe it in instead:\n" +
				"  echo \"$GITHUB_TOKEN\" | runnerly login --with-token\n" +
				"then run `runnerly setup` again")
		}
		return err
	}
	if token == "" {
		return errors.New("no token was entered")
	}

	// Verify before storing, for the same reason `login` does: a token that
	// does not work fails later, somewhere less obvious.
	client := s.env.newGitHubClient(host, token)
	identity, err := client.Identify(ctx)
	if err != nil {
		return fmt.Errorf("the token was not accepted by %s.\n%w", host, err)
	}
	path := s.env.credentialsPath()
	if err := auth.Store(path, host, auth.Host{
		Token:  token,
		User:   identity.User.Login,
		Scopes: identity.Scopes,
	}); err != nil {
		return err
	}

	s.client = client
	s.p.Pass("signed in to %s as %s", host, identity.User.Login)
	s.p.Detail(fmt.Sprintf("Token %s stored in %s (mode 0600), not encrypted.",
		auth.Redact(token), path))
	s.p.Println()
	return nil
}

// chooseScope decides which repository or organization the runner joins,
// asking only when nothing already answers it.
func (s *setup) chooseScope(ctx context.Context) error {
	target, err := s.env.resolveScope(s.scope, s.cfg, "")
	if err == nil {
		s.target = target
		s.p.Pass("runner will join %s", target)
		s.p.Println()
		return nil
	}

	if s.assumeYes {
		return errors.New("no repository was given, and --yes means not asking.\n" +
			"Pass --repo owner/repo or --org login")
	}
	if !s.env.interactive {
		return errors.New("no repository was given and there is no terminal to ask on.\n" +
			"Pass --repo owner/repo or --org login")
	}

	repos, err := s.client.ListRepositories(ctx, github.ListRepositoriesOptions{
		AdminOnly: true,
		Limit:     github.DefaultRepositoryLimit,
	})
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return errors.New("this token cannot administer any repository, so it cannot register a runner.\n" +
			"A classic token needs the `repo` scope; a fine-grained one needs administration access.\n" +
			"Or name one directly with --repo owner/repo")
	}

	return s.pickRepository(repos)
}

// pickRepository asks which repository the runner joins.
//
// GitHub returns them most recently pushed first, which is the best guess
// available at what someone is setting a runner up for, so the first screen
// is simply the top of that list.
//
// An account with hundreds of repositories used to be refused here: the
// picker gave up and told the operator to pass --repo. That is a dead end
// in the one command whose whole purpose is not making you look things up,
// and it is what someone with a lot of repositories hits first. Typing
// anything that is not a number now filters the list instead.
func (s *setup) pickRepository(repos []github.Repository) error {
	matches := repos

	for attempt := 0; ; attempt++ {
		if len(matches) == 0 {
			s.p.Println("  Nothing matched. Showing everything again.")
			s.p.Println()
			matches = repos
		}

		shown := matches
		if len(shown) > maxPickerRepositories {
			shown = shown[:maxPickerRepositories]
		}

		if attempt == 0 {
			s.p.Println("Which repository should this runner serve?")
			s.p.Println()
		}

		for i, r := range shown {
			visibility := "private"
			if !r.Private {
				visibility = "public"
			}
			s.p.Printf("  %2d  %-44s %s\n", i+1, r.FullName, visibility)
		}
		if len(matches) > len(shown) {
			s.p.Dim("  ... and %d more. Type part of a name to narrow it.",
				len(matches)-len(shown))
		}
		s.p.Println()

		answer, err := s.ask(fmt.Sprintf("  Number [1-%d], or type to search: ", len(shown)))
		if err != nil {
			return err
		}
		answer = strings.TrimSpace(answer)

		switch {
		case answer == "":
			return errors.New("nothing was chosen")

		case isNumber(answer):
			n, _ := strconv.Atoi(answer)
			if n < 1 || n > len(shown) {
				s.p.Println()
				s.p.Warn("%d is not one of the numbers listed", n)
				s.p.Println()
				continue
			}
			target, err := github.ParseRepository(shown[n-1].FullName)
			if err != nil {
				return err
			}
			s.target = target
			s.p.Println()
			return nil

		case strings.Contains(answer, "/"):
			// A full owner/repo is an answer, not a search: someone who
			// knows the name should not have to find it in a list.
			target, err := github.ParseRepository(answer)
			if err != nil {
				return err
			}
			s.target = target
			s.p.Println()
			return nil

		default:
			matches = filterRepositories(repos, answer)
			s.p.Println()
			s.p.Printf("  %d matching %q\n", len(matches), answer)
			s.p.Println()
		}
	}
}

// filterRepositories keeps the repositories whose name contains the query,
// case-insensitively, in the order GitHub returned them.
func filterRepositories(repos []github.Repository, query string) []github.Repository {
	query = strings.ToLower(query)

	out := make([]github.Repository, 0, len(repos))
	for _, r := range repos {
		if strings.Contains(strings.ToLower(r.FullName), query) {
			out = append(out, r)
		}
	}
	return out
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// confirmPlan states every change before any of them happens.
func (s *setup) confirmPlan(configFound bool) (bool, error) {
	dir, err := s.installDir()
	if err != nil {
		return false, err
	}
	s.p.Heading("What this will do")
	if !configFound {
		s.p.Printf("  write     %s\n", s.configPath)
	}
	s.p.Printf("  install   GitHub's runner into %s\n", dir)
	s.p.Printf("  register  %s with %s\n", s.name, s.target)
	if labels, err := s.plannedLabels(); err == nil {
		s.p.Printf("  labels    %s\n", strings.Join(labels, ","))
	}
	s.p.Println()
	s.p.Dim("  Nothing is started. The last step prints how to do that.")
	s.p.Println()

	if s.assumeYes {
		return true, nil
	}
	return s.confirm("Go ahead?")
}

// plannedLabels is what the runner will actually register with. It goes
// through the installer's own merge rather than reimplementing it, so the
// preview cannot drift away from what is registered.
func (s *setup) plannedLabels() ([]string, error) {
	// Ask the same Env the install will, not runtime: those agree on a real
	// machine, and a preview that reads a different source is a preview that
	// can lie.
	env := s.env.runnerEnv()
	platform, err := runner.PlatformFor(env.GOOS, env.GOARCH)
	if err != nil {
		return nil, err
	}
	return runner.MergeLabels(platform, splitLabels(s.labels, s.cfg)), nil
}

// installDir is where the runner goes. The plan and the install both call it,
// so what was shown is what happens.
func (s *setup) installDir() (string, error) {
	dir := s.dir
	if dir == "" {
		dir = s.cfg.RunnerDir(s.name)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	return abs, nil
}

func (s *setup) writeConfig() error {
	if err := config.WriteTemplate(s.configPath); err != nil {
		return err
	}
	s.p.Pass("wrote %s", s.configPath)
	return nil
}

func (s *setup) registerRunner(ctx context.Context) error {
	dir, err := s.installDir()
	if err != nil {
		return err
	}

	token, err := s.client.CreateRegistrationToken(ctx, s.target)
	if err != nil {
		return err
	}

	result, err := runner.Install(ctx, s.env.runnerEnv(), s.client, s.target, runner.Options{
		Dir:               dir,
		Name:              s.name,
		Labels:            splitLabels(s.labels, s.cfg),
		URL:               s.target.WebURL(s.cfg.GitHub.Host),
		RegistrationToken: token.Token,
		Progress:          func(step string) { s.p.Dim("  %s", step) },
	})
	if err != nil {
		return err
	}

	record := state.Runner{
		Name:    s.name,
		Scope:   s.target,
		Host:    s.cfg.GitHub.Host,
		Dir:     result.Dir,
		Labels:  result.Labels,
		Release: result.Filename,
	}
	if err := state.Put(s.env.statePath(), record); err != nil {
		// The runner is registered and working. Failing now would be
		// misleading; say what was missed instead.
		s.p.Warn("could not record the runner on this machine: %v", err)
		s.p.Detail("Later commands will need --repo.")
	}

	s.p.Pass("%s is registered with %s", s.name, s.target)
	return nil
}

func (s *setup) finish() {
	s.p.Println()
	s.p.Heading("Ready")
	s.p.Println("Start it now:")
	s.p.Println()
	s.p.Printf("  runnerly agent run %s\n", s.name)
	s.p.Println()
	s.p.Println("Or keep it running across reboots. This prints a unit and the commands")
	s.p.Println("to install it; it does not run them, because they need root:")
	s.p.Println()
	s.p.Printf("  runnerly agent systemd %s\n", s.name)
	s.p.Println()
	s.p.Println("Then send it a job:")
	s.p.Println()
	s.p.Println("  jobs:")
	s.p.Println("    test:")
	if labels, err := s.plannedLabels(); err == nil {
		s.p.Printf("      runs-on: [%s]\n", strings.Join(labels, ", "))
	}
	s.p.Println()
}

// confirm asks a yes/no question through the same reader every other
// prompt uses.
//
// It deliberately does not call the package-level confirm(): that builds a
// reader of its own, and a second reader over the same stdin loses
// whatever the first buffered past its newline. The wizard asks more than
// one question, so it cannot afford that — the answer to the plan was
// being swallowed by the repository picker's read-ahead.
func (s *setup) confirm(prompt string) (bool, error) {
	if !s.env.interactive {
		return false, fmt.Errorf("%s\nThere is no terminal to confirm on.\n"+
			"Re-run with --yes if you are sure", prompt)
	}

	answer, err := s.ask(prompt + " [y/N]: ")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// ask reads one visible line. Secrets go through ui.ReadSecret instead.
//
// The reader is kept on the struct rather than built per call. bufio reads
// ahead, so a fresh reader each time throws away whatever the last one
// buffered past the newline — which is invisible for a single question and
// eats input the moment anything asks twice, as the repository search does.
func (s *setup) ask(prompt string) (string, error) {
	if !s.env.interactive {
		return "", errors.New("there is no terminal to ask on")
	}
	if s.in == nil {
		s.in = bufio.NewReader(s.env.in)
	}

	fmt.Fprint(s.env.out, prompt)
	line, err := s.in.ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) && line == "" {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return line, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
