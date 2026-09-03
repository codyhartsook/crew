// Package roomcmd is the room itself: reading the shared context for where you
// are, and posting, answering and retracting entries in it.
package roomcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		asJSON bool
		inbox  bool
		ack    bool
	)

	cmd := &cobra.Command{
		Use:     "room",
		Aliases: []string{"rooms"},
		Short:   "Show the shared context for where you are",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if ack && !inbox {
				return errors.New("--ack requires --inbox")
			}
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			if inbox {
				author, err := rc.Author(cmd.Context(), "")
				if err != nil {
					return err
				}
				entries, err := rc.Rooms.Unread(cmd.Context(), author)
				if err != nil {
					return err
				}
				if len(entries) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "nothing new")
					return nil
				}
				if asJSON {
					if err := writeJSON(cmd.OutOrStdout(), entries); err != nil {
						return err
					}
				} else {
					authors, err := rc.Authors(cmd.Context())
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), room.Delivery(entries, authors))
				}
				if ack {
					return rc.Rooms.Ack(cmd.Context(), author, entries[len(entries)-1].ID)
				}
				return nil
			}

			entries, err := rc.Rooms.Entries(cmd.Context(), room.Filter{
				Rooms: rc.Keys(),
				Limit: roomctx.BriefingLimit * len(rc.Here) * 3,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), entries)
			}

			// Listing the caller as "also here" is noise; failing to identify
			// it is not a reason to refuse the briefing.
			self, _ := rc.Author(cmd.Context(), "")
			others, err := rc.Others(cmd.Context(), self)
			if err != nil {
				return err
			}
			values, err := rc.Rooms.States(cmd.Context(), room.StateFilter{Rooms: rc.Keys()})
			if err != nil {
				return err
			}
			authors, err := rc.Authors(cmd.Context())
			if err != nil {
				return err
			}
			out := room.Briefing(rc.Here, entries, values, others, authors)
			if out == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: nothing posted yet\n", rc.Here[0].Name)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a briefing")
	cmd.Flags().BoolVar(&inbox, "inbox", false, "show unread addressed entries")
	cmd.Flags().BoolVar(&ack, "ack", false, "mark inbox entries as delivered")
	return cmd
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
