package cli

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/github"
	"github.com/bablilayoub/runnerly/internal/ui"
)

func newRepoCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repo",
		Aliases: []string{"repos"},
		Short:   "Discover repositories you can attach runners to",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRepoListCommand(e))
	return cmd
}

func newRepoListCommand(e *env) *cobra.Command {
	var (
		all    bool
		limit  int
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "list [query]",
		Short: "List repositories, most recently pushed first",
		Long: "list shows the repositories your token can see.\n\n" +
			"By default it shows only repositories you can administer, because adding a\n" +
			"runner requires admin access. Pass --all to see the rest.\n\n" +
			"An optional query filters by a case-insensitive substring of owner/repo.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, _, err := e.githubClient()
			if err != nil {
				return err
			}

			var query string
			if len(args) == 1 {
				query = args[0]
			}

			repos, err := client.SearchRepositories(cmd.Context(), query, github.ListRepositoriesOptions{
				Limit:     limit,
				AdminOnly: !all,
			})
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(e.out)
				enc.SetIndent("", "  ")
				return enc.Encode(repos)
			}

			p := e.printer()
			if len(repos) == 0 {
				p.Skip("no repositories matched")
				if !all {
					p.Detail("Only repositories you can administer are shown. Pass --all to widen the search.")
				}
				return nil
			}

			table := ui.NewTable("REPOSITORY", "VISIBILITY", "ADMIN")
			for _, r := range repos {
				visibility := "public"
				if r.Private {
					visibility = "private"
				}
				if r.Archived {
					visibility += ", archived"
				}
				table.Row(r.FullName, visibility, yesNo(r.Permissions.Admin))
			}
			p.Table(table)
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "include repositories you cannot administer")
	cmd.Flags().IntVar(&limit, "limit", github.DefaultRepositoryLimit, "maximum repositories to fetch")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the list as JSON")
	return cmd
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func joinLabels(names []string) string {
	if len(names) == 0 {
		return "-"
	}
	return strings.Join(names, ",")
}
