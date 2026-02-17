package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/phenixrizen/rift/internal/state"
	"github.com/spf13/cobra"
)

var errSelectionCancelled = errors.New("selection cancelled")

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
			contextMeta := map[string]state.ClusterRecord{}
			for _, c := range st.Clusters {
				if _, ok := seen[c.KubeContext]; ok {
					continue
				}
				seen[c.KubeContext] = struct{}{}
				contexts = append(contexts, c.KubeContext)
				contextMeta[c.KubeContext] = c
			}
			ranks := fuzzy.RankFindNormalizedFold(filter, contexts)
			if len(ranks) == 0 {
				return fmt.Errorf("no context matches %q", filter)
			}
			sort.Sort(ranks)

			selected, err := pickContext(cmd, filter, ranks, contextMeta)
			if err != nil {
				if errors.Is(err, errSelectionCancelled) {
					fmt.Fprintln(cmd.OutOrStdout(), "Selection cancelled.")
					return nil
				}
				return err
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

func pickContext(cmd *cobra.Command, filter string, ranks fuzzy.Ranks, contextMeta map[string]state.ClusterRecord) (string, error) {
	if len(ranks) == 1 {
		return ranks[0].Target, nil
	}
	for _, rank := range ranks {
		if strings.EqualFold(strings.TrimSpace(filter), strings.TrimSpace(rank.Target)) {
			return rank.Target, nil
		}
	}

	const maxOptions = 12
	limit := len(ranks)
	if limit > maxOptions {
		limit = maxOptions
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Multiple contexts match %q:\n", filter)
	for i := 0; i < limit; i++ {
		target := ranks[i].Target
		rec := contextMeta[target]
		fmt.Fprintf(
			out,
			"  %2d) %s  [%s | %s | %s | %s]\n",
			i+1,
			target,
			rec.Env,
			rec.AccountName,
			rec.RoleName,
			rec.ClusterName,
		)
	}
	if len(ranks) > limit {
		fmt.Fprintf(out, "  ...and %d more matches\n", len(ranks)-limit)
	}
	fmt.Fprint(out, "Select a number (Enter/q to cancel): ")

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" || strings.EqualFold(line, "q") {
		return "", errSelectionCancelled
	}

	choice, err := strconv.Atoi(line)
	if err != nil {
		return "", fmt.Errorf("invalid selection %q", line)
	}
	if choice < 1 || choice > limit {
		return "", fmt.Errorf("selection %d out of range (1-%d)", choice, limit)
	}
	return ranks[choice-1].Target, nil
}
