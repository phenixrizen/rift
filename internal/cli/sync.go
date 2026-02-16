package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func newSyncCmd(app *App) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Discover AWS SSO + EKS and sync AWS/kube configs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := app.RunSync(context.Background(), dryRun)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if dryRun {
				println(out, "Dry run complete (no files written)")
			}
			fmt.Fprintf(out, "Discovered roles:    %d\n", len(report.State.Roles))
			fmt.Fprintf(out, "Discovered clusters: %d\n", len(report.State.Clusters))
			if report.NS.Enabled {
				fmt.Fprintf(out, "Namespaces: tried=%d updated=%d errors=%d\n", report.NS.ClustersTried, report.NS.ClustersUpdated, report.NS.Errors)
			}
			fmt.Fprintf(out, "AWS profiles: +%d ~%d -%d\n", report.AWS.Added, report.AWS.Updated, report.AWS.Removed)
			fmt.Fprintf(out, "Kube contexts: +%d ~%d -%d\n", report.Kube.AddedContexts, report.Kube.UpdatedContexts, report.Kube.RemovedContexts)
			if !dryRun {
				fmt.Fprintf(out, "State written: %s\n", app.StatePath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing files")
	return cmd
}
