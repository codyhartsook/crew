package documentcmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/documents"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func NewUnpublish(opts *cmdutil.Options) *cobra.Command {
	var target targetFlags
	cmd := &cobra.Command{
		Use:   "unpublish <name>",
		Short: "Remove a document from this room",
		Long:  "Removes and announces a document. The file moves to the store's trash, not away.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()
			r, author, err := resolve(cmd.Context(), rc, isHuman(), target)
			if err != nil {
				return err
			}
			dir, err := documentDir(opts, r.Key)
			if err != nil {
				return err
			}
			trashed, err := documents.Remove(dir, args[0])
			if err != nil {
				return err
			}
			e := &room.Entry{
				Room: r.Key, Scope: r.Scope, Mode: room.ModeNote, Author: author,
				Body: documents.RemovedNote + args[0], CreatedAt: time.Now().UTC(),
			}
			if err := rc.Rooms.Post(cmd.Context(), e); err != nil {
				return fmt.Errorf("document was removed to %s but its room announcement failed: %w", trashed, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %s (%s)\n", args[0], r.Name, r.Scope)
			return nil
		},
	}
	target.bind(cmd)
	return cmd
}
