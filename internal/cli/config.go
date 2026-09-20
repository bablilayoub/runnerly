package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/bablilayoub/runnerly/internal/config"
)

func newConfigCommand(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and create the Runnerly configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newConfigPathCommand(e),
		newConfigShowCommand(e),
		newConfigInitCommand(e),
		newConfigValidateCommand(e),
	)
	return cmd
}

func newConfigPathCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the configuration file path that would be used",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := e.configPath
			if path == "" {
				path = config.Path()
			}
			fmt.Fprintln(e.out, path)
			return nil
		},
	}
}

func newConfigShowCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective configuration, including defaults",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, path, found, err := e.loadConfig()
			if err != nil {
				return err
			}
			p := e.printer()
			if found {
				p.Dim("# %s", path)
			} else {
				p.Dim("# no file at %s; showing defaults", path)
			}
			out, err := yaml.Marshal(cfg)
			if err != nil {
				return fmt.Errorf("encode config: %w", err)
			}
			fmt.Fprint(e.out, string(out))
			return nil
		},
	}
}

func newConfigInitCommand(e *env) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a default configuration file",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := e.configPath
			if path == "" {
				path = config.Path()
			}
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists.\n"+
					"Nothing was written, so your current settings are intact.\n"+
					"Run `runnerly config init --force` to overwrite it", path)
			}
			if err := config.WriteTemplate(path); err != nil {
				return err
			}
			p := e.printer()
			p.Pass("wrote %s", path)
			p.Detail("Edit it, then run `runnerly doctor` to check the machine.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing configuration file")
	return cmd
}

func newConfigValidateCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check the configuration file for errors",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, path, found, err := e.loadConfig()
			if err != nil {
				return err
			}
			if err := config.Validate(cfg); err != nil {
				return fmt.Errorf("%s is not valid.\n%w", path, err)
			}
			p := e.printer()
			if found {
				p.Pass("%s is valid", path)
			} else {
				p.Pass("no file at %s; the built-in defaults are valid", path)
			}
			return nil
		},
	}
}
