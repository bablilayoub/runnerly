// Package cli implements the runnerly command line interface.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/config"
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
	out        io.Writer
	errOut     io.Writer
	configPath string
	noColor    bool
}

// printer returns a Printer for stdout honoring the --no-color flag.
func (e *env) printer() *ui.Printer {
	if e.noColor {
		return ui.NewPlain(e.out)
	}
	return ui.New(e.out)
}

// loadConfig resolves and reads the configuration, reporting which path was
// used so commands can tell the operator.
func (e *env) loadConfig() (cfg config.Config, path string, found bool, err error) {
	path = e.configPath
	if path == "" {
		path = config.Path()
	}
	cfg, found, err = config.Load(path)
	return cfg, path, found, err
}

// NewRootCommand builds the command tree writing to the given streams.
func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	e := &env{out: out, errOut: errOut}

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
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().StringVar(&e.configPath, "config", "",
		"path to config.yaml (default: $RUNNERLY_CONFIG, else ~/.config/runnerly/config.yaml)")
	root.PersistentFlags().BoolVar(&e.noColor, "no-color", false, "disable colored output")
	root.SetVersionTemplate("{{.Version}}\n")
	root.Version = version.Get().Short()

	root.AddCommand(
		newVersionCommand(e),
		newDoctorCommand(e),
		newConfigCommand(e),
	)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := NewRootCommand(os.Stdout, os.Stderr)
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
