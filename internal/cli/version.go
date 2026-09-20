package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bablilayoub/runnerly/internal/version"
)

func newVersionCommand(e *env) *cobra.Command {
	var (
		short  bool
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := version.Get()
			switch {
			case asJSON:
				enc := json.NewEncoder(e.out)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			case short:
				fmt.Fprintln(e.out, info.Short())
			default:
				fmt.Fprintln(e.out, info.String())
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&short, "short", false, "print only the version number")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print build information as JSON")
	return cmd
}
