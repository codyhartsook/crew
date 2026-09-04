// Package clearcmd deletes the entries in a room.
package clearcmd

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		confirm  bool
		alsoRepo bool
	)
	cmd := &cobra.Command{
		Use:    "clear",
		Short:  "Delete the entries in this room",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			author, err := rc.Author(cmd.Context(), "")
			if err != nil {
				return err
			}
			keys, err := rc.Accessible(cmd.Context(), author)
			if err != nil {
				return err
			}

			targets := rc.Here[:1]
			if alsoRepo {
				targets = rc.Here
			}
			for _, r := range targets {
				if !slices.Contains(keys, r.Key) {
					return fmt.Errorf("the active agent has not joined %s", r.Name)
				}
				entries, err := rc.Rooms.Entries(cmd.Context(), room.Filter{Rooms: []string{r.Key}})
				if err != nil {
					return err
				}
				if !confirm {
					fmt.Fprintf(cmd.OutOrStdout(), "%s (%s): %d entries would be deleted\n", r.Name, r.Scope, len(entries))
					continue
				}
				n, err := rc.Rooms.Clear(cmd.Context(), r.Key)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %d entries deleted\n", r.Name, n)
			}
			if !confirm {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing deleted; re-run with --yes")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "yes", false, "actually delete")
	cmd.Flags().BoolVar(&alsoRepo, "repo", false, "also clear the repository room")
	return cmd
}
