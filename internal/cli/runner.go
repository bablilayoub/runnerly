package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/ui"
)

func newRunnerCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "runner",
		Aliases: []string{"runners"},
		Short:   "Manage self-hosted runners",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newRunnerCreateCommand(e),
		newRunnerListCommand(e),
		newRunnerStatusCommand(e),
		newRunnerRemoveCommand(e),
	)
	return cmd
}

func newRunnerListCommand(e *env) *cobra.Command {
	var (
		scope  scopeFlags
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List runners registered with GitHub",
		Long: "list shows the self-hosted runners GitHub has registered for a repository\n" +
			"or organization.\n\n" +
			"This is GitHub's view, not Runnerly's: it includes runners Runnerly did not\n" +
			"create, and a runner shows as offline whenever its machine is not connected.",
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

			runners, err := client.ListRunners(cmd.Context(), target)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(e.out)
				enc.SetIndent("", "  ")
				return enc.Encode(runners)
			}

			p := e.printer()
			if len(runners) == 0 {
				p.Skip("no runners registered for %s", target)
				p.Detail("Create one with `runnerly runner create`.")
				return nil
			}

			table := ui.NewTable("NAME", "ID", "STATUS", "OS", "LABELS")
			for _, r := range runners {
				table.Row(
					r.Name,
					strconv.FormatInt(r.ID, 10),
					r.State(),
					r.OS,
					joinLabels(r.LabelNames()),
				)
			}
			p.Table(table)
			return nil
		},
	}

	scope.register(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the list as JSON")
	return cmd
}

func newRunnerStatusCommand(e *env) *cobra.Command {
	var (
		scope  scopeFlags
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "status <name>",
		Short: "Show one runner in detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, _, err := e.githubClient()
			if err != nil {
				return err
			}
			target, err := scope.resolve(cfg)
			if err != nil {
				return err
			}

			runner, err := client.FindRunnerByName(cmd.Context(), target, args[0])
			if err != nil {
				return describeRunnerLookup(err, target, args[0])
			}

			if asJSON {
				enc := json.NewEncoder(e.out)
				enc.SetIndent("", "  ")
				return enc.Encode(runner)
			}

			p := e.printer()
			p.Heading(runner.Name)
			p.Println("scope     ", target.String())
			p.Println("id        ", strconv.FormatInt(runner.ID, 10))
			p.Println("status    ", runner.State())
			p.Println("os        ", runner.OS)
			p.Println("labels    ", joinLabels(runner.LabelNames()))
			if runner.Ephemeral {
				p.Println("ephemeral  yes")
			}
			return nil
		},
	}

	scope.register(cmd)
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the runner as JSON")
	return cmd
}

func newRunnerRemoveCommand(e *env) *cobra.Command {
	var (
		scope     scopeFlags
		id        int64
		assumeYes bool
	)

	cmd := &cobra.Command{
		Use:     "remove [name]",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a runner's registration from GitHub",
		Long: "remove deletes a runner's registration from GitHub.\n\n" +
			"It does not stop a runner process that is still running on a machine, and it\n" +
			"does not delete anything from that machine. A still-running runner whose\n" +
			"registration has been removed will fail to reconnect.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && id == 0 {
				return errors.New("name the runner to remove, or pass --id")
			}
			if len(args) == 1 && id != 0 {
				return errors.New("pass a name or --id, not both")
			}

			client, cfg, _, err := e.githubClient()
			if err != nil {
				return err
			}
			target, err := scope.resolve(cfg)
			if err != nil {
				return err
			}

			label := "runner " + strconv.FormatInt(id, 10)
			if len(args) == 1 {
				runner, err := client.FindRunnerByName(cmd.Context(), target, args[0])
				if err != nil {
					return describeRunnerLookup(err, target, args[0])
				}
				id, label = runner.ID, runner.Name
			}

			p := e.printer()
			if !assumeYes {
				ok, err := confirm(e.in, e.out, e.interactive,
					fmt.Sprintf("Remove %s from %s?", label, target))
				if err != nil {
					return err
				}
				if !ok {
					p.Skip("nothing was removed")
					return nil
				}
			}

			if err := client.DeleteRunner(cmd.Context(), target, id); err != nil {
				return err
			}
			p.Pass("removed %s from %s", label, target)
			p.Detail("If the runner is still running on a machine, stop it there too.")
			return nil
		},
	}

	scope.register(cmd)
	cmd.Flags().Int64Var(&id, "id", 0, "remove by GitHub runner ID instead of name")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "do not ask for confirmation")
	return cmd
}

// describeRunnerLookup turns "not found" into advice, and passes anything else
// through untouched.
func describeRunnerLookup(err error, scope github.Scope, name string) error {
	if errors.Is(err, github.ErrRunnerNotFound) {
		return fmt.Errorf("no runner named %q is registered for %s.\nRun `runnerly runner list` to see what is", name, scope)
	}
	return err
}
