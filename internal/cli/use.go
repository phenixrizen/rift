package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/spf13/cobra"
)

func newUseCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use <filter>",
		Short: "Fuzzy-match and switch kubectl context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := args[0]
			st, err := app.loadState()
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("state file not found; run: rift sync")
				}
				return err
			}
			if len(st.Clusters) == 0 {
				return fmt.Errorf("no contexts available; run: rift sync")
			}

			contexts := make([]string, 0, len(st.Clusters))
			seen := map[string]struct{}{}
			for _, c := range st.Clusters {
				if _, ok := seen[c.KubeContext]; ok {
					continue
				}
				seen[c.KubeContext] = struct{}{}
				contexts = append(contexts, c.KubeContext)
			}
			ranks := fuzzy.RankFindNormalizedFold(filter, contexts)
			if len(ranks) == 0 {
				return fmt.Errorf("no context matches %q", filter)
			}
			sort.Sort(ranks)
			selected := ranks[0].Target

			if len(ranks) > 1 && strings.EqualFold(ranks[0].Target, ranks[1].Target) {
				return fmt.Errorf("ambiguous context for %q", filter)
			}

			run := exec.CommandContext(context.Background(), "kubectl", "config", "use-context", selected)
			run.Stdout = cmd.OutOrStdout()
			run.Stderr = cmd.ErrOrStderr()
			if err := run.Run(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Switched context: %s\n", selected)
			return nil
		},
	}
	return cmd
}
