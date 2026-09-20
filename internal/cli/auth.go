package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/auth"
)

func newLoginCommand(e *env) *cobra.Command {
	var (
		withToken bool
		host      string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store a GitHub token for this machine",
		Long: "login verifies a GitHub token and stores it for later commands.\n\n" +
			"The token is read from standard input rather than a flag, so it does not\n" +
			"reach your shell history or the process table:\n\n" +
			"  echo \"$GITHUB_TOKEN\" | runnerly login --with-token\n" +
			"  runnerly login --with-token < token.txt\n\n" +
			"Registering repository runners needs a classic token with the `repo` scope,\n" +
			"or a fine-grained token with administration access. Organization runners need\n" +
			"`admin:org`.\n\n" +
			"To avoid storing a token at all, set RUNNERLY_GITHUB_TOKEN instead: an\n" +
			"environment token always wins over the stored one.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, _, err := e.loadConfig()
			if err != nil {
				return err
			}
			if host == "" {
				host = cfg.GitHub.Host
			}

			if !withToken {
				return fmt.Errorf("no token was supplied.\n"+
					"Runnerly reads the token from standard input so it stays out of your shell\n"+
					"history. Create one at https://%s/settings/tokens, then run:\n"+
					"  echo \"$GITHUB_TOKEN\" | runnerly login --with-token", host)
			}

			token, err := auth.ReadToken(e.in)
			if err != nil {
				return err
			}

			// Verify before storing. A token that does not work is worse than
			// no token: it fails later, somewhere less obvious.
			identity, err := e.newGitHubClient(host, token).Identify(cmd.Context())
			if err != nil {
				return fmt.Errorf("the token was not accepted by %s.\n%w", host, err)
			}

			path := e.credentialsPath()
			if err := auth.Store(path, host, auth.Host{
				Token:  token,
				User:   identity.User.Login,
				Scopes: identity.Scopes,
			}); err != nil {
				return err
			}

			p := e.printer()
			p.Pass("logged in to %s as %s", host, identity.User.Login)
			p.Detail(fmt.Sprintf("Token %s stored in %s (mode 0600).", auth.Redact(token), path))
			p.Detail("The file is not encrypted. To keep the token off disk, unset it there and\n" +
				"set " + auth.EnvVarNames()[0] + " instead.")
			if identity.FineGrained {
				p.Detail("GitHub reported no scopes, which means this is a fine-grained token.\n" +
					"Runnerly cannot check its permissions in advance.")
			} else {
				p.Detail("Scopes: " + strings.Join(identity.Scopes, ", "))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&withToken, "with-token", false, "read the token from standard input")
	cmd.Flags().StringVar(&host, "host", "", "GitHub host (default: github.host from the configuration)")
	return cmd
}

func newLogoutCommand(e *env) *cobra.Command {
	var (
		host      string
		assumeYes bool
	)

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored GitHub token",
		Long: "logout deletes the token this machine has stored for a GitHub host.\n\n" +
			"It does not revoke the token on GitHub. Revoke it there if it may have\n" +
			"been exposed.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _, _, err := e.loadConfig()
			if err != nil {
				return err
			}
			if host == "" {
				host = cfg.GitHub.Host
			}
			path := e.credentialsPath()
			p := e.printer()

			if !assumeYes {
				ok, err := confirm(e.in, e.out, e.interactive,
					fmt.Sprintf("Remove the stored token for %s?", host))
				if err != nil {
					return err
				}
				if !ok {
					p.Skip("nothing was removed")
					return nil
				}
			}

			removed, err := auth.Delete(path, host)
			if err != nil {
				return err
			}
			if !removed {
				p.Skip("no token was stored for %s", host)
				return nil
			}
			p.Pass("removed the stored token for %s", host)
			p.Detail("This did not revoke it on GitHub. Revoke it at\n" +
				"https://" + host + "/settings/tokens if it may have been exposed.")
			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "GitHub host (default: github.host from the configuration)")
	cmd.Flags().BoolVar(&assumeYes, "yes", false, "do not ask for confirmation")
	return cmd
}

func newAuthCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect GitHub authentication",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAuthStatusCommand(e))
	return cmd
}

// authStatus is the JSON shape of `runnerly auth status --json`.
type authStatus struct {
	Host          string   `json:"host"`
	Authenticated bool     `json:"authenticated"`
	User          string   `json:"user,omitempty"`
	Source        string   `json:"source,omitempty"`
	Origin        string   `json:"origin,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	FineGrained   bool     `json:"fine_grained,omitempty"`
	Error         string   `json:"error,omitempty"`
}

func newAuthStatusCommand(e *env) *cobra.Command {
	var (
		asJSON  bool
		offline bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which GitHub credential is in use",
		Long: "status reports which token Runnerly would use and whether GitHub still\n" +
			"accepts it. It exits 1 when there is no usable credential.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, _, err := e.loadConfig()
			if err != nil {
				return err
			}
			host := cfg.GitHub.Host
			status := authStatus{Host: host}

			token, err := e.githubToken(host)
			if err != nil {
				status.Error = err.Error()
				return e.renderAuthStatus(status, asJSON)
			}
			status.Source = string(token.Source)
			status.Origin = token.Origin
			status.User = token.User

			if offline {
				status.Authenticated = true
				return e.renderAuthStatus(status, asJSON)
			}

			identity, err := e.newGitHubClient(host, token.Value).Identify(cmd.Context())
			if err != nil {
				status.Error = err.Error()
				return e.renderAuthStatus(status, asJSON)
			}
			status.Authenticated = true
			status.User = identity.User.Login
			status.Scopes = identity.Scopes
			status.FineGrained = identity.FineGrained
			return e.renderAuthStatus(status, asJSON)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print the status as JSON")
	cmd.Flags().BoolVar(&offline, "offline", false, "report the stored credential without contacting GitHub")
	return cmd
}

// renderAuthStatus prints the status and returns an ExitError when there is no
// usable credential, so scripts can branch on it.
func (e *env) renderAuthStatus(s authStatus, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(e.out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			return err
		}
	} else {
		p := e.printer()
		if !s.Authenticated {
			p.Fail("%s", s.Host)
			p.Detail(s.Error)
			p.Remedy("runnerly login --with-token")
			return &ExitError{Code: 1}
		}

		p.Pass("%s", s.Host)
		if s.User != "" {
			p.Detail("account: " + s.User)
		}
		if s.Origin != "" {
			p.Detail(fmt.Sprintf("token from %s (%s)", s.Origin, s.Source))
		}
		switch {
		case s.FineGrained:
			p.Detail("fine-grained token; GitHub does not report its scopes")
		case len(s.Scopes) > 0:
			p.Detail("scopes: " + strings.Join(s.Scopes, ", "))
		}
	}

	if !s.Authenticated {
		return &ExitError{Code: 1}
	}
	return nil
}
