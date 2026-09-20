package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/secret"
	"github.com/bablilayoub/runnerly/internal/store"
	"github.com/bablilayoub/runnerly/internal/ui"
)

// envDatabaseURL keeps the database password out of the configuration file.
const envDatabaseURL = "RUNNERLY_DATABASE_URL"

func newServerCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Operate the Runnerly control plane",
		Long: "server holds the commands an operator runs on the machine that hosts the\n" +
			"control plane.\n\n" +
			"These talk to PostgreSQL directly rather than through the API. That is\n" +
			"deliberate: issuing the first enrollment token cannot require signing in to\n" +
			"a dashboard that itself needs OAuth configured, or there would be no way to\n" +
			"bootstrap a new server.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newServerKeygenCommand(e),
		newServerMigrateCommand(e),
		newEnrollmentTokenCommand(e),
	)
	return cmd
}

// openStore connects to the database the server uses.
func (e *env) openStore(ctx context.Context) (*store.Store, error) {
	cfg, _, _, err := e.loadConfig()
	if err != nil {
		return nil, err
	}

	url := os.Getenv(envDatabaseURL)
	if url == "" {
		url = cfg.Server.Database
	}
	if url == "" {
		return nil, fmt.Errorf("no database is configured.\n"+
			"Set server.database in %s, or %s in the environment",
			e.resolvedConfigPath(), envDatabaseURL)
	}
	return store.Open(ctx, store.Options{URL: url})
}

func newServerKeygenCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "keygen",
		Short: "Generate a secret key for encrypting stored credentials",
		Long: "keygen prints a new 32-byte key.\n\n" +
			"The control plane uses it to encrypt signed-in users' GitHub tokens at\n" +
			"rest. Changing it makes anything sealed with the old key unreadable, so\n" +
			"keep it with your other secrets.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			key, err := secret.GenerateKey()
			if err != nil {
				return err
			}
			fmt.Fprintln(e.out, key)

			p := e.printer()
			p.Dim("# Set it as RUNNERLY_SECRET_KEY, or as server.secret_key in the configuration.")
			p.Dim("# Store it somewhere you can recover it: credentials sealed with a lost key")
			p.Dim("# cannot be read back.")
			return nil
		},
	}
}

func newServerMigrateCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply database migrations",
		Long: "migrate brings the database schema up to date.\n\n" +
			"runnerly-server does this on start, so this is for running migrations\n" +
			"separately from a deployment, or for checking what is outstanding.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := e.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			applied, err := db.Migrate(cmd.Context())
			if err != nil {
				return err
			}

			p := e.printer()
			if len(applied) == 0 {
				p.Pass("the schema is up to date")
				return nil
			}
			p.Pass("applied %d migration(s)", len(applied))
			for _, name := range applied {
				p.Detail(name)
			}
			return nil
		},
	}
}

func newEnrollmentTokenCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "enrollment-token",
		Aliases: []string{"enrollment-tokens"},
		Short:   "Issue and revoke the tokens machines enroll with",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newEnrollmentTokenCreateCommand(e),
		newEnrollmentTokenListCommand(e),
		newEnrollmentTokenRevokeCommand(e),
	)
	return cmd
}

func newEnrollmentTokenCreateCommand(e *env) *cobra.Command {
	var (
		description string
		maxUses     int
		expiresIn   time.Duration
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Issue a token a machine can enroll with",
		Long: "create issues an enrollment token.\n\n" +
			"The secret is printed once and never stored: the server keeps only its\n" +
			"hash, so a database dump cannot be turned back into a working credential.\n" +
			"Give it to a machine through RUNNERLY_ENROLLMENT_TOKEN.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if maxUses < 0 {
				return errors.New("--max-uses must not be negative (0 means unlimited)")
			}

			db, err := e.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			var uses *int
			if maxUses > 0 {
				uses = &maxUses
			}
			var expiresAt *time.Time
			if expiresIn > 0 {
				t := time.Now().UTC().Add(expiresIn)
				expiresAt = &t
			}

			token, plaintext, err := db.CreateEnrollmentToken(cmd.Context(), description, uses, expiresAt)
			if err != nil {
				return err
			}

			p := e.printer()
			p.Pass("created enrollment token %s", token.ID)
			p.Println()
			fmt.Fprintln(e.out, plaintext)
			p.Println()
			p.Dim("# This is the only time the secret is shown.")
			p.Dim("# On the runner machine:")
			p.Dim("#   export %s='%s'", "RUNNERLY_ENROLLMENT_TOKEN", "<the value above>")
			p.Dim("#   runnerly agent run")
			if uses == nil {
				p.Warn("this token has no use limit; pass --max-uses to bound it")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "what this token is for, shown in listings")
	cmd.Flags().IntVar(&maxUses, "max-uses", 1, "how many machines may enroll with it (0 for unlimited)")
	cmd.Flags().DurationVar(&expiresIn, "expires-in", 24*time.Hour, "how long it stays valid (0 for never)")
	return cmd
}

func newEnrollmentTokenListCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List issued enrollment tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := e.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			tokens, err := db.ListEnrollmentTokens(cmd.Context())
			if err != nil {
				return err
			}

			p := e.printer()
			if len(tokens) == 0 {
				p.Skip("no enrollment tokens have been issued")
				p.Detail("Create one with `runnerly server enrollment-token create`.")
				return nil
			}

			now := time.Now()
			table := ui.NewTable("ID", "DESCRIPTION", "USES", "STATE", "CREATED")
			for _, t := range tokens {
				uses := strconv.Itoa(t.Uses)
				if t.MaxUses != nil {
					uses += "/" + strconv.Itoa(*t.MaxUses)
				}
				table.Row(t.ID, orDash(t.Description), uses, tokenState(t, now),
					t.CreatedAt.Format(time.RFC3339))
			}
			p.Table(table)
			return nil
		},
	}
}

// tokenState summarizes why a token can or cannot be used.
func tokenState(t store.EnrollmentToken, now time.Time) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case t.ExpiresAt != nil && !now.Before(*t.ExpiresAt):
		return "expired"
	case t.MaxUses != nil && t.Uses >= *t.MaxUses:
		return "used up"
	default:
		return "usable"
	}
}

func newEnrollmentTokenRevokeCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Stop an enrollment token being used again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := e.openStore(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			if err := db.RevokeEnrollmentToken(cmd.Context(), args[0]); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("no usable enrollment token has id %q.\n"+
						"It may already be revoked; run `runnerly server enrollment-token list`", args[0])
				}
				return err
			}
			e.printer().Pass("revoked %s", args[0])
			return nil
		},
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
