package rolecmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func newDeactivate(opts *cmdutil.Options) *cobra.Command {
	var toRepo bool

	cmd := &cobra.Command{
		Use:   "deactivate <name>",
		Short: "Turn off a role for this room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			rs, ok := rc.Store.(store.RoleStore)
			if !ok {
				return errors.New("this store does not support role activation")
			}
			target, err := rc.Target(toRepo)
			if err != nil {
				return err
			}
			if err := rs.DeactivateRole(cmd.Context(), target.Key, name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%q deactivated in %s\n", name, target.Name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "deactivate for the repository room")
	return cmd
}
