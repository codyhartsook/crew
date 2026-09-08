package roomcmd

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
)

func NewRemove(opts *cmdutil.Options) *cobra.Command {
	var as string

	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove one of your unthreaded entries",
		Long: `Deletes an entry you posted, so a mistake does not stay in the room.

Only your own entries, and only while nothing has threaded onto them: an answer
cannot be removed, and neither can a request once it has been answered. Post a
correction instead. Removal is permanent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
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
			if !rc.Has(target.Room) {
				return fmt.Errorf("entry [%d] is not in this room", id)
			}
			author, err := rc.Author(cmd.Context(), as)
			if err != nil {
				return err
			}
			keys, err := rc.Accessible(cmd.Context(), author)
			if err != nil {
				return err
			}
			if !slices.Contains(keys, target.Room) {
				return fmt.Errorf("entry [%d] is not in this room", id)
			}
			removed, err := rc.Rooms.RemoveEntry(cmd.Context(), id, author)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("entry [%d] is not yours or is part of a thread", id)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed [%d]\n", id)
			return nil
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	_ = cmd.Flags().MarkHidden("as")
	return cmd
}
