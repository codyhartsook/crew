package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func newWhoAmICmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "whoami",
		Aliases: []string{"me"},
		Short:   "Print this agent's friendly name",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			name, err := ownAlias(cmd.Context(), rc.store, room.Keys(rc.here))
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), name)
			return nil
		},
	}
}

func ownAlias(ctx context.Context, st store.Store, roomKeys []string) (string, error) {
	key, err := resolveAuthor(ctx, st, roomKeys)
	if err != nil {
		return "", err
	}
	sess, err := st.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return displayAgent(sess), nil
}
