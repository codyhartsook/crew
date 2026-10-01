// Package roomcmd is the room itself: reading the shared context for where you
// are, and posting, answering and retracting entries in it.
package roomcmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		asJSON bool
		last   int
	)

	cmd := &cobra.Command{
		Use:     "room",
		Aliases: []string{"rooms"},
		Short:   "Show the shared context for where you are",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if last < 0 {
				return fmt.Errorf("--last must not be negative")
			}
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			self, err := rc.Author(cmd.Context(), "")
			if err != nil {
				return err
			}
			keys, err := rc.Accessible(cmd.Context(), self)
			if err != nil {
				return err
			}
			// An empty room list means unfiltered in the store, so a
			// non-member must never reach the query.
			if len(keys) == 0 {
				if asJSON {
					return cmdutil.WriteJSON(cmd.OutOrStdout(), []*room.Entry{})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: no rooms here for this session\n", rc.Here[0].Name)
				return nil
			}
			entries, err := rc.Rooms.Entries(cmd.Context(), room.Filter{Rooms: keys, Limit: last})
			if err != nil {
				return err
			}
			// The cursor is a high-water mark: acking the newest entry of a truncated
			// view would silence the older requests it hid.
			truncated := last > 0 && len(entries) == last
			ack := func() error {
				if self == "" || len(entries) == 0 || truncated {
					return nil
				}
				return rc.Rooms.Ack(cmd.Context(), self, entries[len(entries)-1].ID)
			}
			if asJSON {
				if err := cmdutil.WriteJSON(cmd.OutOrStdout(), entries); err != nil {
					return err
				}
				return ack()
			}
			others, err := rc.Others(cmd.Context(), self)
			if err != nil {
				return err
			}
			authors, err := rc.Authors(cmd.Context())
			if err != nil {
				return err
			}
			out := room.Briefing(rc.Here, entries, others, authors)
			if out == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: nothing posted yet\n", rc.Here[0].Name)
				return ack()
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return ack()
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a briefing")
	cmd.Flags().IntVar(&last, "last", 0, "show only the newest N entries")
	return cmd
}
