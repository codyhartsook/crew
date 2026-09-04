// Package stopcmd gracefully stops the local notification broker.
package stopcmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/registry"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Gracefully stop the local broker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Server != "" {
				return fmt.Errorf("stop controls the local broker; unset --server")
			}
			baseURL := "http://" + addr
			if !registry.IsUp(cmd.Context(), baseURL) {
				fmt.Fprintln(cmd.OutOrStdout(), "broker is not running")
				return nil
			}
			if err := registry.Stop(cmd.Context(), baseURL); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "broker stopped at %s\n", baseURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", registry.DefaultAddr, "address of the local broker")
	return cmd
}
