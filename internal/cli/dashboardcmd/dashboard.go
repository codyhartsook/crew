// Package dashboardcmd opens the dashboard the local registry serves.
package dashboardcmd

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/server"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		addr   string
		noOpen bool
	)

	cmd := &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"fleet", "ui"},
		Short:   "Open the dashboard",
		Long:    "Opens the dashboard served by crew init.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// Already pointed at a remote registry: there is nothing to start.
			if opts.Server != "" {
				fmt.Fprintf(out, "dashboard  %s\n", opts.Server)
				return OpenDashboard(cmd.Context(), opts.Server, noOpen)
			}

			baseURL := "http://" + addr
			if !server.IsUp(cmd.Context(), baseURL) {
				return fmt.Errorf("dashboard is not running; start it with crew init")
			}
			fmt.Fprintf(out, "dashboard  %s\n", baseURL)
			return OpenDashboard(cmd.Context(), baseURL, noOpen)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", server.DefaultAddr, "address of the local broker")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the dashboard")
	return cmd
}

const macOSBundleID = "com.codyhartsook.crew"

// OpenDashboard prefers the Crew macOS app for a local dashboard and falls
// back to the system browser when the app is unavailable.
func OpenDashboard(ctx context.Context, rawURL string, skip bool) error {
	return openDashboard(ctx, rawURL, skip, runtime.GOOS, openMacOSApp, OpenBrowser)
}

type dashboardOpener func(context.Context, string, bool) error

func openDashboard(ctx context.Context, rawURL string, skip bool, goos string, app, browser dashboardOpener) error {
	if skip {
		return nil
	}
	if goos == "darwin" && isLocalHTTP(rawURL) {
		if err := app(ctx, rawURL, false); err == nil {
			return nil
		}
	}
	return browser(ctx, rawURL, false)
}

func openMacOSApp(ctx context.Context, rawURL string, _ bool) error {
	dashboardURL := &url.URL{Scheme: "crew", Host: "dashboard"}
	query := dashboardURL.Query()
	query.Set("url", rawURL)
	dashboardURL.RawQuery = query.Encode()
	return exec.CommandContext(ctx, "open", "-b", macOSBundleID, dashboardURL.String()).Run()
}

func isLocalHTTP(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// OpenBrowser asks the desktop to open url. Failing to open a browser is worth
// reporting but never worth failing the command over, since the URL is printed.
func OpenBrowser(ctx context.Context, url string, skip bool) error {
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
