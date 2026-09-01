package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/room"
)

// searchLimit caps each half of a result set.
const searchLimit = 15

func newSearchCmd(opts *options) *cobra.Command {
	var (
		all    bool
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Find runbooks, state and entries by topic",
		Long: `Search this room for a term, across state keys and values and entry bodies.

Use it before starting anything multi-step: a runbook for the task may already
exist, and somebody may have recorded why the obvious approach does not work.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			q := room.Query{Text: strings.Join(args, " "), Limit: searchLimit}
			if !all {
				q.Rooms = room.Keys(rc.here)
			}
			results, err := rc.rooms.Search(cmd.Context(), q)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}
			if results.Empty() {
				fmt.Fprintf(out, "nothing matching %q\n", q.Text)
				return nil
			}

			// Runbooks first: they answer "how do I do this" more directly than
			// an entry that happened to mention it.
			var procs, facts []*room.State
			for _, v := range results.State {
				if v.IsProcedure() {
					procs = append(procs, v)
					continue
				}
				facts = append(facts, v)
			}

			if len(procs) > 0 {
				fmt.Fprintln(out, "procedures")
				for _, v := range procs {
					fmt.Fprintf(out, "  %s\n    %s (%d lines)\n    multiplayer state get %s\n",
						strings.TrimPrefix(v.Key, room.ProcedurePrefix), v.Summary(), v.Lines(), v.Key)
				}
			}
			if len(facts) > 0 {
				fmt.Fprintln(out, "state")
				for _, v := range facts {
					fmt.Fprintf(out, "  %s = %s\n", v.Key, truncate(v.Value, 64))
				}
			}
			if len(results.Entries) > 0 {
				fmt.Fprintln(out, "entries")
				for _, e := range results.Entries {
					ref := e.Anchor.Ref()
					if ref != "" {
						ref = " " + ref
					}
					fmt.Fprintf(out, "  [%d] %-8s%s %s\n", e.ID, e.Kind, ref, truncate(e.Body, 64))
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "every room, not only this one")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}
