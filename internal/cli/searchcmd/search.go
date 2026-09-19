// Package searchcmd finds room entries by topic.
package searchcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

// searchLimit caps each half of a result set.
const searchLimit = 15

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Find room entries by topic",
		Long: `Search this room for a term across entry bodies.

Use it before starting anything multi-step: somebody may already have recorded
why the obvious approach does not work.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			out := cmd.OutOrStdout()
			// An empty room list means unfiltered in the store, so a
			// non-member must never reach the query.
			if len(keys) == 0 {
				if asJSON {
					return cmdutil.WriteJSON(out, []*room.Entry{})
				}
				fmt.Fprintln(out, "no rooms here for this session")
				return nil
			}
			q := room.Query{Text: strings.Join(args, " "), Rooms: keys, Limit: searchLimit}
			entries, err := rc.Rooms.Search(cmd.Context(), q)
			if err != nil {
				return err
			}
			if asJSON {
				return cmdutil.WriteJSON(out, entries)
			}
			if len(entries) == 0 {
				fmt.Fprintf(out, "nothing matching %q\n", q.Text)
				return nil
			}

			for _, e := range entries {
				fmt.Fprintf(out, "[%d] %-8s %s\n", e.ID, e.Mode, cmdutil.Truncate(e.Body, 64))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
