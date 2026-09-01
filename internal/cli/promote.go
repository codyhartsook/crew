package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/room"
)

func newPromoteCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "promote <id|state-key>...",
		Short: "Move something from this worktree's room to the repository's",
		Long: `Move something out of this worktree's room into the wider repository room,
for when a finding turns out to be about the repository rather than the task.

A numeric argument is an entry; anything else is a state key. A thread moves
whole: an entry takes anything that resolves it.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			from := rc.here[0]
			to := roomFor(rc, true)
			if to.Key == from.Key {
				return fmt.Errorf("this is a primary checkout, so its worktree and repository are one room (%s); there is nowhere to promote to", from.Name)
			}

			out := cmd.OutOrStdout()
			for _, arg := range args {
				moved, err := promoteOne(cmd, rc, arg, from, to)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", moved)
			}
			fmt.Fprintf(out, "\nnow in %s, visible to every worktree of this repository\n", to.Name)
			return nil
		},
	}
}

func promoteOne(cmd *cobra.Command, rc *roomContext, arg string, from, to room.Room) (string, error) {
	ctx := cmd.Context()

	if id, err := strconv.ParseInt(arg, 10, 64); err == nil && id > 0 {
		entry, err := entryByID(ctx, rc.rooms, id)
		if err != nil {
			return "", err
		}
		n, err := rc.rooms.Promote(ctx, id, to.Key, to.Scope)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("[%d] %s promoted%s", id, entry.Kind, alsoMoved(n-1, "reply", "replies")), nil
	}

	if err := rc.rooms.PromoteState(ctx, from.Key, arg, to.Key, to.Scope); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s promoted", arg), nil
}

// alsoMoved names what came along with the thing being promoted.
func alsoMoved(n int, one, many string) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return " with 1 " + one
	default:
		return fmt.Sprintf(" with %d %s", n, many)
	}
}
