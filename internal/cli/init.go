package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/phenixrizen/rift/internal/config"
	"github.com/phenixrizen/rift/internal/discovery"
	"github.com/spf13/cobra"
)

func newInitCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Interactively initialize Rift config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			defaults := config.Default()
			if cfg, err := app.loadConfig(); err == nil {
				defaults = cfg
			}
			if defaults.SSORegion == "" {
				defaults.SSORegion = "us-east-1"
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			startURL, err := prompt(reader, cmd.OutOrStdout(), "SSO start URL", defaults.SSOStartURL)
			if err != nil {
				return err
			}
			ssoRegion, err := prompt(reader, cmd.OutOrStdout(), "SSO region", defaults.SSORegion)
			if err != nil {
				return err
			}

			defaults.SSOStartURL = strings.TrimSpace(startURL)
			defaults.SSORegion = strings.TrimSpace(strings.ToLower(ssoRegion))

			if err := config.Save(app.ConfigPath, defaults); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Wrote config: %s\n", app.ConfigPath)
			err = discovery.ValidateSSOLogin(defaults, time.Now().UTC())
			if err == nil {
				println(cmd.OutOrStdout(), "SSO token is present.", "Initialization complete.")
				return nil
			}
			if errors.Is(err, discovery.ErrSSONotLoggedIn) {
				println(cmd.OutOrStdout(), "SSO token not found or expired.", "Run: rift auth")
				return nil
			}
			return err
		},
	}
	return cmd
}

func prompt(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		if !errors.Is(err, io.EOF) {
			return "", err
		}
	}
	value := strings.TrimSpace(line)
	if value == "" {
		value = defaultValue
	}
	return value, nil
}
