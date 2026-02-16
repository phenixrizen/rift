package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/phenixrizen/rift/internal/tableview"
	"github.com/spf13/cobra"
)

func newListCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List known Rift contexts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := app.loadState()
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("state file not found; run: rift sync")
				}
				return err
			}
			if len(st.Clusters) == 0 {
				println(cmd.OutOrStdout(), "No clusters discovered.", "Run: rift sync")
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), tableview.RenderClusters(st.Clusters))
			return nil
		},
	}
	return cmd
}
