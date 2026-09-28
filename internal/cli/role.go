package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/cli/skill"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func newRole(opts *cmdutil.Options) *cobra.Command {
	var drop bool

	cmd := &cobra.Command{
		Use:   `role <name> "<description>"`,
		Short: "Take the role the user gave you",
		Long: `Publishes a skill telling agents in this room what to send you. Describe
which tasks to send and what you won't take. Assigning again replaces it;
--drop gives it up. It ends with your session.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if drop {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.MinimumNArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rc, err := roomctx.Open(ctx, opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			roles, ok := rc.Store.(store.RoleStore)
			if !ok {
				return errors.New("this store does not support roles")
			}
			self, err := rc.Author(ctx, "")
			if err != nil {
				return err
			}
			if _, err := rc.Accessible(ctx, self); err != nil {
				return err
			}
			sess, err := rc.Store.Get(ctx, self)
			if err != nil {
				return err
			}
			alias, target := room.Display(sess), rc.Here[0]

			out := cmd.OutOrStdout()
			if drop {
				had, err := roles.Drop(ctx, self)
				if err != nil {
					return err
				}
				if !had {
					fmt.Fprintf(out, "%s holds no role\n", alias)
					return nil
				}
				fmt.Fprintf(out, "%s dropped its role\n", alias)
			} else {
				r := &store.Role{
					SessionKey: self, Room: target.Key, Name: args[0],
					Description: strings.TrimSpace(strings.Join(args[1:], " ")), AssignedAt: time.Now().UTC(),
				}
				if err := roles.Assign(ctx, r); err != nil {
					return err
				}
				fmt.Fprintf(out, "%s holds the %s role in %s\n", alias, r.Name, target.Name)
			}

			// A sandbox may block the skills directories; the next hook or broker sweep writes them.
			home, err := os.UserHomeDir()
			if err == nil {
				err = skill.SyncRoles(ctx, home, rc.Store)
			}
			if err != nil {
				if !errors.Is(err, fs.ErrPermission) {
					fmt.Fprintln(cmd.ErrOrStderr(), "crew: update role skills:", err)
				}
				fmt.Fprintln(out, "the skill will update shortly")
			}
			opts.SignalBroker()
			return nil
		},
	}

	cmd.Flags().BoolVar(&drop, "drop", false, "give up your role")
	return cmd
}
