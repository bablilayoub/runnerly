package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/jobstate"
	"github.com/bablilayoub/runnerly/internal/runner"
	"github.com/bablilayoub/runnerly/internal/state"
	"github.com/bablilayoub/runnerly/internal/ui"
	"github.com/bablilayoub/runnerly/internal/version"
)

func newUpgradeCommand(e *env) *cobra.Command {
	var (
		scope     scopeFlags
		doRunners bool
		assumeYes bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Check for newer Runnerly and GitHub runner versions",
		Long: "upgrade reports what is out of date and, with --runners, brings this\n" +
			"machine's runners up to the release GitHub currently expects.\n\n" +
			"It never upgrades anything on its own. An upgrade restarts a runner, and\n" +
			"restarting one that is mid-job throws that job away, so it is something to\n" +
			"ask for rather than something to have happen.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, _, err := e.loadConfig()
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

			p := e.printer()
			p.Heading("Updates")

			// Runnerly itself.
			current := version.Get().Short()
			latest, err := latestRunnerlyRelease(cmd.Context(), client)
			switch {
			case err != nil:
				p.Skip("Runnerly %s", current)
				p.Detail("Could not check for a newer release: " + err.Error())
			case latest == "":
				p.Skip("Runnerly %s", current)
				p.Detail("No releases have been published yet, so there is nothing to compare against.")
			case latest == current:
				p.Pass("Runnerly %s is current", current)
			default:
				p.Warn("Runnerly %s → %s", current, latest)
				p.Detail("Runnerly does not replace its own binary. Download the release and\n" +
					"install it the way you installed this one.")
			}

			// The GitHub runner.
			available, err := runnerReleaseFor(cmd.Context(), client, target, e.runnerEnv())
			if err != nil {
				p.Fail("could not ask GitHub which runner release it expects")
				p.Detail(err.Error())
				return nil
			}

			installed, err := state.List(e.statePath())
			if err != nil {
				return err
			}
			if len(installed) == 0 {
				p.Skip("no runners are installed on this machine")
				p.Detail("GitHub currently expects runner " + available + ".")
				return nil
			}

			table := ui.NewTable("RUNNER", "INSTALLED", "AVAILABLE", "")
			var stale []state.Runner
			for _, r := range installed {
				have := releaseVersion(r.Release)
				status := "current"
				if have != "" && have != available {
					status = "update available"
					stale = append(stale, r)
				} else if have == "" {
					status = "unknown"
				}
				table.Row(r.Name, orDashVersion(have), available, status)
			}
			p.Println()
			p.Table(table)

			if len(stale) == 0 {
				p.Println()
				p.Pass("every installed runner is current")
				return nil
			}
			if !doRunners {
				p.Println()
				p.Dim("Upgrade them with: runnerly upgrade --runners")
				return nil
			}

			return e.upgradeRunners(cmd.Context(), client, stale, assumeYes)
		},
	}

	scope.register(cmd)
	cmd.Flags().BoolVar(&doRunners, "runners", false, "upgrade the runners installed on this machine")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "do not ask for confirmation")
	return cmd
}

// upgradeRunners reinstalls each stale runner at the current release.
func (e *env) upgradeRunners(ctx context.Context, client *github.Client,
	stale []state.Runner, assumeYes bool,
) error {
	p := e.printer()

	names := make([]string, 0, len(stale))
	for _, r := range stale {
		names = append(names, r.Name)
	}

	if !assumeYes {
		ok, err := confirm(e.in, e.out, e.interactive, fmt.Sprintf(
			"Upgrade %s? Each runner is stopped and re-registered, and a job running on one is lost.",
			strings.Join(names, ", ")))
		if err != nil {
			return err
		}
		if !ok {
			p.Skip("nothing was upgraded")
			return nil
		}
	}

	for _, r := range stale {
		// A runner mid-job would lose it. The hooks know; without them
		// there is no way to check, which the message says.
		if job, err := jobstate.Read(jobstate.Path(r.Dir)); err == nil && job.Running() {
			p.Warn("skipped %s: it is running %s", r.Name, job.Describe())
			continue
		}

		p.Dim("  upgrading %s", r.Name)
		token, err := client.CreateRegistrationToken(ctx, r.Scope)
		if err != nil {
			p.Fail("could not upgrade %s: %v", r.Name, err)
			continue
		}

		// Removing the unpacked runner is what makes this an upgrade: the
		// installer reuses whatever is already there.
		if err := removeUnpackedRunner(r.Dir); err != nil {
			p.Fail("could not clear %s: %v", r.Dir, err)
			continue
		}

		result, err := runner.Install(ctx, e.runnerEnv(), client, r.Scope, runner.Options{
			Dir:               r.Dir,
			Name:              r.Name,
			Labels:            r.Labels,
			URL:               r.Scope.WebURL(r.Host),
			RegistrationToken: token.Token,
			Ephemeral:         r.Ephemeral,
			Replace:           true,
			Progress:          func(step string) { p.Dim("    %s", step) },
		})
		if err != nil {
			p.Fail("could not upgrade %s: %v", r.Name, err)
			continue
		}

		r.Release = result.Filename
		r.Labels = result.Labels
		if err := state.Put(e.statePath(), r); err != nil {
			p.Warn("upgraded %s but could not record it: %v", r.Name, err)
		}
		p.Pass("%s is now %s", r.Name, releaseVersion(result.Filename))
	}

	p.Println()
	p.Detail("Restart the agents so they pick up the new binaries.")
	return nil
}

// releasePattern pulls the version out of a runner archive name, which looks
// like actions-runner-linux-x64-2.337.0.tar.gz.
var releasePattern = regexp.MustCompile(`-(\d+\.\d+\.\d+)\.(?:tar\.gz|zip)$`)

// releaseVersion returns the version in an archive filename, or "".
func releaseVersion(filename string) string {
	if m := releasePattern.FindStringSubmatch(filename); len(m) == 2 {
		return m[1]
	}
	return ""
}

func orDashVersion(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// runnerReleaseFor asks GitHub which runner release it expects here.
func runnerReleaseFor(ctx context.Context, client *github.Client, scope github.Scope, env runner.Env) (string, error) {
	platform, err := runner.PlatformFor(env.GOOS, env.GOARCH)
	if err != nil {
		return "", err
	}
	download, err := client.FindDownload(ctx, scope, platform.OS, platform.Arch)
	if err != nil {
		return "", err
	}
	if v := releaseVersion(download.Filename); v != "" {
		return v, nil
	}
	return download.Filename, nil
}

// latestRunnerlyRelease reports the newest published Runnerly release.
//
// An empty string with no error means the project has published none, which
// is the honest answer rather than pretending everything is up to date.
func latestRunnerlyRelease(ctx context.Context, client *github.Client) (string, error) {
	release, err := client.LatestRelease(ctx, "bablilayoub", "runnerly")
	if err != nil {
		if errors.Is(err, github.ErrNoRelease) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimPrefix(release, "v"), nil
}
