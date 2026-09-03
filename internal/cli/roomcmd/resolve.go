package roomcmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func NewResolve(opts *cmdutil.Options) *cobra.Command {
	var as string

	cmd := &cobra.Command{
		Use:   "resolve <id> <body>",
		Short: "Answer or close an open entry",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid entry id %q", args[0])
			}
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()

			target, err := rc.Entry(cmd.Context(), id)
			if err != nil {
				return err
			}
			author, err := rc.Author(cmd.Context(), as)
			if err != nil {
				return err
			}

			// A resolution inherits the room and kind of what it closes so the
			// pair reads as one thread.
			e := &room.Entry{
				Room:      target.Room,
				Scope:     target.Scope,
				Kind:      target.Kind,
				Author:    author,
				Body:      strings.Join(args[1:], " "),
				Resolves:  target.ID,
				CreatedAt: time.Now().UTC(),
			}
			if err := rc.Rooms.Post(cmd.Context(), e); err != nil {
				return err
			}
			opts.SignalBroker()
			fmt.Fprintf(cmd.OutOrStdout(), "resolved [%d] with [%d]\n", target.ID, e.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	return cmd
}
