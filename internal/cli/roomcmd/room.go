// Package roomcmd is the room itself: reading the shared context for where you
// are, and posting, answering and retracting entries in it.
package roomcmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:     "room",
		Aliases: []string{"rooms"},
		Short:   "Show the shared context for where you are",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			entries, err := rc.Rooms.Entries(cmd.Context(), room.Filter{Rooms: keys})
			if err != nil {
				return err
			}
			// Listing the caller as "also here" is noise.
			ack := func() error {
				if self == "" || len(entries) == 0 {
					return nil
				}
				return rc.Rooms.Ack(cmd.Context(), self, entries[len(entries)-1].ID)
			}
			if asJSON {
				if err := writeJSON(cmd.OutOrStdout(), entries); err != nil {
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
	return cmd
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
