// Package servecmd runs the registry in the foreground.
package servecmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/registry"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		addr    string
		verbose bool
	)

	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Serve the registry API and dashboard over HTTP",
		Hidden: true,
		Long: `Reads and writes the same local database the CLI uses. Setting
MULTIPLAYER_SERVER points hooks at it, so they write through the API instead of
opening the database directly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			return registry.Serve(cmd.Context(), opts, registry.Config{
				Addr:    addr,
				Verbose: verbose,
				Log:     cmd.ErrOrStderr(),
				OnReady: func(baseURL string) {
					fmt.Fprintf(out, "dashboard  %s\n", baseURL)
					fmt.Fprintf(out, "hooks      export MULTIPLAYER_SERVER=%s\n", baseURL)
				},
			})
		},
	}

	cmd.Flags().StringVar(&addr, "addr", registry.DefaultAddr, "address to listen on")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log every request")
	return cmd
}
