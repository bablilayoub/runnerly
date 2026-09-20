package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/doctor"
)

func newDoctorCommand(e *env) *cobra.Command {
	var (
		asJSON  bool
		offline bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check whether this machine can host a runner",
		Long: "doctor inspects the local machine and reports what would stop a\n" +
			"GitHub Actions runner from working here.\n\n" +
			"It never requires a Runnerly control plane: checks that need one\n" +
			"are skipped. It exits 1 if any check fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, path, found, err := e.loadConfig()
			if err != nil {
				return err
			}

			report := doctor.Run(cmd.Context(), doctor.Options{
				Config:      cfg,
				ConfigPath:  path,
				ConfigFound: found,
				Offline:     offline,
				Timeout:     timeout,
				Env:         doctor.DefaultEnv(),
			})

			if asJSON {
				if err := doctor.RenderJSON(e.out, report); err != nil {
					return err
				}
			} else {
				doctor.Render(e.printer(), report)
			}

			if !report.OK() {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON")
	cmd.Flags().BoolVar(&offline, "offline", false, "skip checks that need the network")
	cmd.Flags().DurationVar(&timeout, "timeout", doctor.DefaultTimeout, "per-check timeout")
	return cmd
}
