// Package dashboardcmd opens the dashboard the local registry serves.
package dashboardcmd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/registry"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		addr   string
		noOpen bool
	)

	cmd := &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"fleet", "ui"},
		Short:   "Open the dashboard in a browser",
		Long:    "Opens the dashboard served by crew init.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// Already pointed at a remote registry: there is nothing to start.
			if opts.Server != "" {
				fmt.Fprintf(out, "dashboard  %s\n", opts.Server)
				return openBrowser(cmd.Context(), opts.Server, noOpen)
			}

			baseURL := "http://" + addr
			if !registry.IsUp(cmd.Context(), baseURL) {
				return fmt.Errorf("dashboard is not running; start it with crew init")
			}
			fmt.Fprintf(out, "dashboard  %s\n", baseURL)
			return openBrowser(cmd.Context(), baseURL, noOpen)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", registry.DefaultAddr, "address of the local broker")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open a browser")
	return cmd
}

// openBrowser asks the desktop to open url. Failing to open a browser is worth
// reporting but never worth failing the command over, since the URL is printed.
func openBrowser(ctx context.Context, url string, skip bool) error {
	if skip {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	return cmd.Start()
}
