package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/phenixrizen/rift/internal/awsconfig"
	"github.com/spf13/cobra"
)

func newAuthCmd(app *App) *cobra.Command {
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Run AWS IAM Identity Center (SSO) login",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthFlow(app, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), noBrowser)
		},
	}

	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Use AWS device auth flow without opening a browser")
	return cmd
}

func runAuthFlow(app *App, stdin io.Reader, stdout, stderr io.Writer, noBrowser bool) error {
	cfg, err := app.loadConfig()
	if err != nil {
		return err
	}

	awsConfigPath, err := defaultAWSConfigPath()
	if err != nil {
		return err
	}
	if _, err := awsconfig.EnsureSession(awsConfigPath, cfg, false); err != nil {
		return fmt.Errorf("prepare aws sso session: %w", err)
	}

	args := []string{
		"sso",
		"login",
		"--sso-session",
		"rift",
	}
	if noBrowser {
		args = append(args, "--no-browser")
	}
	println(
		stdout,
		"Starting AWS SSO login...",
		"If prompted, approve application: botocore-client-rift",
	)

	output, err := runAWS(stdin, args...)
	if len(output) > 0 {
		_, _ = io.WriteString(stderr, string(output))
	}
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return fmt.Errorf("aws CLI not found in PATH")
		}
		if supportsOnlyProfile(output) {
			if _, ensureErr := awsconfig.EnsureLegacyAuthProfile(awsConfigPath, cfg, false); ensureErr != nil {
				return fmt.Errorf("prepare legacy aws sso profile: %w", ensureErr)
			}
			fallbackArgs := []string{"sso", "login", "--profile", "rift-auth"}
			if noBrowser {
				fallbackArgs = append(fallbackArgs, "--no-browser")
			}
			println(
				stdout,
				"Detected older AWS CLI login mode.",
				"If prompted, approve application: botocore-client-rift-auth",
			)
			fallbackOutput, fallbackErr := runAWS(stdin, fallbackArgs...)
			if len(fallbackOutput) > 0 {
				_, _ = io.WriteString(stderr, string(fallbackOutput))
			}
			if fallbackErr == nil {
				println(stdout, "SSO login complete.", "You can now run: rift sync")
				return nil
			}
			return fmt.Errorf("aws sso login failed: %w", fallbackErr)
		}
		return fmt.Errorf("aws sso login failed: %w", err)
	}

	println(stdout, "SSO login complete.", "You can now run: rift sync")
	return nil
}

func runAWS(stdin io.Reader, args ...string) ([]byte, error) {
	run := exec.CommandContext(context.Background(), "aws", args...)
	run.Stdin = stdin
	return run.CombinedOutput()
}

func supportsOnlyProfile(output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "unknown options") && strings.Contains(text, "--sso-session")
}
