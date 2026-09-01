package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/room"
)

func newStateCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "state",
		Aliases: []string{"st"},
		Short:   "Read and write what is currently true in this room",
		Long: `Entries record that something happened; state records what is currently true.
A value is replaced in place, so a reader learns it without folding a history.
Keys namespace themselves with a prefix, as in "build/status".

If how a value changed matters, post a decision alongside it.`,
	}
	cmd.AddCommand(
		newStateSetCmd(opts),
		newStateGetCmd(opts),
		newStateListCmd(opts),
		newStateRemoveCmd(opts),
	)
	return cmd
}

func newStateSetCmd(opts *options) *cobra.Command {
	var (
		toRepo bool
		as     string
	)

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a value, replacing any previous one",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			author, err := authorFor(cmd.Context(), rc, as)
			if err != nil {
				return err
			}
			target := roomFor(rc, toRepo)
			st := &room.State{
				Room: target.Key, Scope: target.Scope,
				Key: args[0], Value: strings.Join(args[1:], " "),
				Author: author, UpdatedAt: time.Now().UTC(),
			}
			if err := rc.rooms.SetState(cmd.Context(), st); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s = %s  (%s, revision %d)\n",
				st.Key, st.Value, target.Name, st.Revision)
			return nil
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "write to the repository room")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	return cmd
}

func newStateGetCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print one value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			// The worktree's own value wins over the repository's, so a
			// worktree can override what the repository says.
			var lastErr error
			for _, r := range rc.here {
				st, err := rc.rooms.GetState(cmd.Context(), r.Key, args[0])
				if err == nil {
					fmt.Fprintln(cmd.OutOrStdout(), st.Value)
					return nil
				}
				lastErr = err
			}
			return lastErr
		},
	}
}

func newStateListCmd(opts *options) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:     "ls [prefix]",
		Aliases: []string{"list"},
		Short:   "List the values in this room",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			f := room.StateFilter{Rooms: room.Keys(rc.here)}
			if len(args) == 1 {
				f.Prefix = args[0]
			}
			if all {
				f.Rooms = nil
			}
			values, err := rc.rooms.States(cmd.Context(), f)
			if err != nil {
				return err
			}
			if len(values) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no values")
				return nil
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "KEY\tVALUE\tREV\tBY\tUPDATED")
			for _, st := range values {
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
					st.Key, truncate(st.Value, 52), st.Revision, room.Author(st.Author), room.Ago(st.UpdatedAt))
			}
			return tw.Flush()
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "every room, not only this one")
	return cmd
}

func newStateRemoveCmd(opts *options) *cobra.Command {
	var toRepo bool

	cmd := &cobra.Command{
		Use:     "rm <key>",
		Aliases: []string{"remove"},
		Short:   "Delete a value",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			target := roomFor(rc, toRepo)
			if err := rc.rooms.DeleteState(cmd.Context(), target.Key, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s from %s\n", args[0], target.Name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "delete from the repository room")
	return cmd
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
