package cli

import (
	"fmt"

	"github.com/phenixrizen/rift/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print Rift version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version.ResolveCommit())
			return err
		},
	}
}
