package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/autumnsgrove/meridian/internal/config"
)

// newDocsCommand groups the generated reference text. Anything that lives in both the code and the
// documents is printed from the code's own table, so the two cannot drift.
func newDocsCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "docs",
		Short: "print the generated reference documentation",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(&cobra.Command{
		Use:   "kinds",
		Short: "print the probe and vital vocabulary",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			fmt.Fprint(command.OutOrStdout(), config.KindsDoc())
			return nil
		},
	})
	return command
}
