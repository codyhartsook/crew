package cli

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// probeTimeout bounds the check for a registry already listening on the address.
const probeTimeout = 750 * time.Millisecond

func newFleetCmd(opts *options) *cobra.Command {
	var (
		addr    string
		noOpen  bool
		verbose bool
	)

	cmd := &cobra.Command{
		Use:     "fleet",
		Aliases: []string{"dashboard", "ui"},
		Short:   "Open the fleet dashboard in a browser",
		Long: `Starts the registry and opens the dashboard. If one is already
listening on the address, opens against it rather than starting a second.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			// Already pointed at a remote registry: there is nothing to start.
			if opts.server != "" {
				fmt.Fprintf(out, "dashboard  %s\n", opts.server)
				return openBrowser(cmd.Context(), opts.server, noOpen)
			}

			// A registry already on this address is the common case when the
			// command is run a second time.
			baseURL := "http://" + addr
			if registryIsUp(cmd.Context(), baseURL) {
				fmt.Fprintf(out, "dashboard  %s  (already running)\n", baseURL)
				return openBrowser(cmd.Context(), baseURL, noOpen)
			}

			// The dashboard only reads; waking is left to serve --wake.
			return serveRegistry(cmd, opts, addr, verbose, false, func(url string) {
				fmt.Fprintf(out, "dashboard  %s\n", url)
				fmt.Fprintln(out, "ctrl-c to stop")
				if err := openBrowser(cmd.Context(), url, noOpen); err != nil {
					fmt.Fprintln(cmd.ErrOrStderr(), "could not open a browser:", err)
				}
			})
		},
	}

	cmd.Flags().StringVar(&addr, "addr", defaultAddr, "address to listen on")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open a browser")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log every request")
	return cmd
}

// registryIsUp reports whether a healthy registry already answers at baseURL.
func registryIsUp(ctx context.Context, baseURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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
