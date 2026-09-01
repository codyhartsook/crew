package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/room"
)

func newClearCmd(opts *options) *cobra.Command {
	var (
		confirm  bool
		alsoRepo bool
	)

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Delete the entries in this room",
		Long: `Without --yes nothing is deleted; the entries that would go are
listed instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			targets := rc.here[:1]
			if alsoRepo {
				targets = rc.here
			}

			out := cmd.OutOrStdout()
			total := 0
			for _, r := range targets {
				entries, err := rc.rooms.Entries(cmd.Context(), room.Filter{Rooms: []string{r.Key}})
				if err != nil {
					return err
				}
				if len(entries) == 0 {
					fmt.Fprintf(out, "%s: already empty\n", r.Name)
					continue
				}
				total += len(entries)
				if !confirm {
					fmt.Fprintf(out, "%s (%s): %d entries would be deleted\n", r.Name, r.Scope, len(entries))
					for _, e := range entries {
						fmt.Fprintf(out, "  [%d] %s: %s\n", e.ID, e.Kind, e.Body)
					}
					continue
				}
				n, err := rc.rooms.Clear(cmd.Context(), r.Key)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s: %d entries deleted\n", r.Name, n)
			}
			if total > 0 && !confirm {
				fmt.Fprintln(out, "\nnothing deleted; re-run with --yes")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "yes", false, "actually delete")
	cmd.Flags().BoolVar(&alsoRepo, "repo", false, "also clear the repository room")
	return cmd
}
