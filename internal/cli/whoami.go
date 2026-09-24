package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
)

func newWhoami(opts *cmdutil.Options) *cobra.Command {
	return &cobra.Command{
		Use:     "whoami",
		Aliases: []string{"me"},
		Short:   "Print this agent's friendly name",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()

			name, err := roomctx.OwnAlias(cmd.Context(), rc.Store, rc.Keys())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), name)
			return nil
		},
	}
}
