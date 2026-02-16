package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/phenixrizen/rift/internal/graphview"
	"github.com/spf13/cobra"
)

func newGraphCmd(app *App) *cobra.Command {
	opts := graphview.Options{Env: "all", Depth: 3}
	var format string
	var maxWidth int

	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Render discovered topology as ASCII or JSON graph",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := app.loadState()
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("state file not found; run: rift sync")
				}
				return err
			}
			if opts.Env == "" {
				opts.Env = "all"
			}
			if opts.Env == "stg" {
				opts.Env = "staging"
			}
			if opts.Env != "all" && opts.Env != "prod" && opts.Env != "staging" && opts.Env != "dev" && opts.Env != "int" && opts.Env != "other" {
				return fmt.Errorf("--env must be one of prod|staging|dev|int|other|all")
			}
			if opts.Depth != 2 && opts.Depth != 3 && opts.Depth != 4 {
				return fmt.Errorf("--depth must be one of 2|3|4")
			}

			graph := graphview.Build(st, opts)
			switch strings.ToLower(format) {
			case "ascii", "":
				fmt.Fprint(cmd.OutOrStdout(), graphview.RenderASCII(graph, maxWidth))
				return nil
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(graph)
			default:
				return fmt.Errorf("invalid --format %q (expected ascii|json)", format)
			}
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", opts.Env, "Filter environment (prod|staging|dev|int|other|all)")
	cmd.Flags().StringVar(&opts.Account, "account", "", "Filter account by name or ID substring")
	cmd.Flags().StringVar(&opts.Role, "role", "", "Filter role by substring")
	cmd.Flags().StringVar(&opts.Region, "region", "", "Filter region")
	cmd.Flags().StringVar(&opts.Cluster, "cluster", "", "Filter cluster by substring")
	cmd.Flags().BoolVar(&opts.Namespaces, "namespaces", false, "Include namespaces layer when depth allows")
	cmd.Flags().IntVar(&opts.Depth, "depth", opts.Depth, "Depth 2|3|4")
	cmd.Flags().StringVar(&format, "format", "ascii", "Output format ascii|json")
	cmd.Flags().IntVar(&maxWidth, "max-width", 120, "Maximum output width")
	return cmd
}
