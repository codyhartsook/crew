// Package statecmd reads and writes what is currently true in a room.
package statecmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/cli/view"
	"github.com/codyhartsook/multiplayer/internal/room"
)

func New(opts *cmdutil.Options) *cobra.Command {
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

// stateColumns is what state ls shows. A revision only matters to a person
// watching a value change; an agent reads the value itself.
var stateColumns = []view.Column{
	{Name: "KEY"},
	{Name: "VALUE"},
	{Name: "BY"},
	{Name: "UPDATED"},
	{Name: "REV", Human: true},
}

// valueWidth clips a listed value. A terminal needs it narrow; an agent can
// take more before reaching for state get.
func valueWidth(human bool) int {
	if human {
		return 52
	}
	return 160
}

func newStateSetCmd(opts *cmdutil.Options) *cobra.Command {
	var (
		toRepo bool
		as     string
	)

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Write a value, replacing any previous one",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()

			author, err := rc.Author(cmd.Context(), as)
			if err != nil {
				return err
			}
			target := rc.Target(toRepo)
			st := &room.State{
				Room: target.Key, Scope: target.Scope,
				Key: args[0], Value: strings.Join(args[1:], " "),
				Author: author, UpdatedAt: time.Now().UTC(),
			}
			if err := rc.Rooms.SetState(cmd.Context(), st); err != nil {
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

func newStateGetCmd(opts *cmdutil.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print one value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()

			// The worktree's own value wins over the repository's, so a
			// worktree can override what the repository says.
			var lastErr error
			for _, r := range rc.Here {
				st, err := rc.Rooms.GetState(cmd.Context(), r.Key, args[0])
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

func newStateListCmd(opts *cmdutil.Options) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:     "ls [prefix]",
		Aliases: []string{"list"},
		Short:   "List the values in this room",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()

			f := room.StateFilter{Rooms: rc.Keys()}
			if len(args) == 1 {
				f.Prefix = args[0]
			}
			if all {
				f.Rooms = nil
			}
			values, err := rc.Rooms.States(cmd.Context(), f)
			if err != nil {
				return err
			}
			if len(values) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no values")
				return nil
			}
			authors, err := rc.Authors(cmd.Context())
			if err != nil {
				return err
			}

			rows := make([][]string, 0, len(values))
			for _, st := range values {
				rows = append(rows, []string{
					st.Key,
					cmdutil.Truncate(st.Value, valueWidth(opts.Human)),
					authors.Name(st.Author),
					room.Ago(st.UpdatedAt),
					strconv.FormatInt(int64(st.Revision), 10),
				})
			}
			return view.Table(cmd.OutOrStdout(), opts.Human, stateColumns, rows)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "every room, not only this one")
	return cmd
}

func newStateRemoveCmd(opts *cmdutil.Options) *cobra.Command {
	var toRepo bool

	cmd := &cobra.Command{
		Use:     "rm <key>",
		Aliases: []string{"remove"},
		Short:   "Delete a value",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, err := roomctx.Open(cmd.Context(), opts, roomctx.Cwd())
			if err != nil {
				return err
			}
			defer rc.Close()

			target := rc.Target(toRepo)
			if err := rc.Rooms.DeleteState(cmd.Context(), target.Key, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %s from %s\n", args[0], target.Name)
			return nil
		},
	}

	cmd.Flags().BoolVar(&toRepo, "repo", false, "delete from the repository room")
	return cmd
}
