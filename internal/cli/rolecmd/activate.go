package rolecmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func newActivate(opts *cmdutil.Options) *cobra.Command {
	var toRepo bool

	cmd := &cobra.Command{
		Use:   "activate <name>",
		Short: "Turn on a defined role for this room",
		Long: `Adds the role to the roster a SessionStart hook injects into every
session in this room. Activation is explicit opt-in: a role stays dormant,
defined but unused, until this is run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			reg, err := discover(cmd.Context())
			if err != nil {
				return err
			}
			if _, ok := reg.Get(name); !ok {
				return fmt.Errorf("no role named %q is defined", name)
			}

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
			if err := rs.ActivateRole(cmd.Context(), &role.Activation{
				Room: target.Key, Role: name, ActivatedAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%q active in %s\n", name, target.Name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "activate for the repository room")
	return cmd
}
