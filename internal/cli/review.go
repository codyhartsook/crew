package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/room"
)

func newReviewCmd(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Record and read code reviews",
		Long: `A review is a batch of findings over a named target. Findings are ordinary
room entries, so each one resolves on its own and carries the reason it was
fixed or declined - which is the record a markdown report cannot hold.`,
	}
	cmd.AddCommand(
		newReviewStartCmd(opts),
		newReviewAddCmd(opts),
		newReviewShowCmd(opts),
		newReviewListCmd(opts),
	)
	return cmd
}

func newReviewStartCmd(opts *options) *cobra.Command {
	var (
		target string
		as     string
		toRepo bool
	)

	cmd := &cobra.Command{
		Use:   "start <summary>",
		Short: "Open a review and print its id",
		Args:  cobra.MinimumNArgs(1),
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
			target := targetOf(rc, target)
			v := &room.Review{
				Room:      roomFor(rc, toRepo).Key,
				Author:    author,
				Target:    target,
				Summary:   strings.Join(args, " "),
				CreatedAt: time.Now().UTC(),
			}
			if err := rc.rooms.StartReview(cmd.Context(), v); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s opened over %s\n", v.Label(), v.Target)
			fmt.Fprintf(cmd.OutOrStdout(), "add findings with: multiplayer review add %s --file <path> --line <n> --severity <must|should|consider> \"<body>\"\n", v.Label())
			return nil
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "what is under review (default: the current HEAD)")
	cmd.Flags().BoolVar(&toRepo, "repo", false, "post to the repository room")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	return cmd
}

func newReviewAddCmd(opts *options) *cobra.Command {
	var (
		file     string
		symbol   string
		line     int
		severity string
		as       string
	)

	cmd := &cobra.Command{
		Use:   "add <review-id> <body>",
		Short: "Add a finding to a review",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseReviewID(args[0])
			if err != nil {
				return err
			}
			sev := room.Severity(severity)
			if !sev.Valid() {
				return fmt.Errorf("unknown severity %q: want must, should, or consider", severity)
			}

			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			v, err := reviewByID(cmd.Context(), rc, id)
			if err != nil {
				return err
			}
			author, err := authorFor(cmd.Context(), rc, as)
			if err != nil {
				return err
			}

			e := &room.Entry{
				Room:      v.Room,
				Scope:     scopeOfRoom(rc, v.Room),
				Kind:      room.KindReview,
				Author:    author,
				Body:      strings.Join(args[1:], " "),
				ReviewID:  v.ID,
				Severity:  sev,
				CreatedAt: time.Now().UTC(),
			}
			if file != "" || symbol != "" || line > 0 {
				e.Anchor = &room.Anchor{File: file, Symbol: symbol, Line: line, BlobSHA: rc.head}
			}
			if err := rc.rooms.Post(cmd.Context(), e); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "[%d] %s finding added to %s %s\n", e.ID, sev, v.Label(), e.Anchor.Ref())
			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "file the finding is in")
	cmd.Flags().StringVar(&symbol, "symbol", "", "function or type, which survives edits that line numbers do not")
	cmd.Flags().IntVar(&line, "line", 0, "line number, a hint only")
	cmd.Flags().StringVar(&severity, "severity", string(room.SeverityShould), "must, should, or consider")
	cmd.Flags().StringVar(&as, "as", "", "session key, if not inferable")
	return cmd
}

func newReviewShowCmd(opts *options) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show <review-id>",
		Short: "Render a review as markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseReviewID(args[0])
			if err != nil {
				return err
			}
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			v, err := reviewByID(cmd.Context(), rc, id)
			if err != nil {
				return err
			}
			entries, err := rc.rooms.Entries(cmd.Context(), room.Filter{ReviewID: v.ID})
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"review": v, "entries": entries})
			}
			fmt.Fprint(cmd.OutOrStdout(), room.Report(v, entries))
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of markdown")
	return cmd
}

func newReviewListCmd(opts *options) *cobra.Command {
	var openOnly bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List reviews in this room",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rc, closeFn, err := openRoomContext(cmd.Context(), opts, cwdOf(cmd))
			if err != nil {
				return err
			}
			defer closeFn()

			reviews, err := rc.rooms.Reviews(cmd.Context(), room.ReviewFilter{
				Rooms: room.Keys(rc.here), OpenOnly: openOnly,
			})
			if err != nil {
				return err
			}
			if len(reviews) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no reviews")
				return nil
			}
			for _, v := range reviews {
				fmt.Fprintf(cmd.OutOrStdout(), "%-5s %d/%d open  %s  %s — %s\n",
					v.Label(), v.Open, v.Findings, dash(v.Target), room.Author(v.Author), v.Summary)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&openOnly, "open", false, "only reviews with findings outstanding")
	return cmd
}

// parseReviewID accepts both "r3" and "3".
func parseReviewID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "r"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid review id %q", s)
	}
	return id, nil
}

func reviewByID(ctx context.Context, rc *roomContext, id int64) (*room.Review, error) {
	found, err := rc.rooms.Reviews(ctx, room.ReviewFilter{IDs: []int64{id}})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no review r%d", id)
	}
	return found[0], nil
}

// roomFor picks the worktree room, or the repository room when asked.
func roomFor(rc *roomContext, toRepo bool) room.Room {
	if !toRepo {
		return rc.here[0]
	}
	for _, r := range rc.here {
		if r.Scope == room.ScopeRepo {
			return r
		}
	}
	// A primary checkout is one place, so the two rooms are the same.
	target := rc.here[0]
	target.Scope = room.ScopeRepo
	return target
}

func scopeOfRoom(rc *roomContext, key string) room.Scope {
	for _, r := range rc.here {
		if r.Key == key {
			return r.Scope
		}
	}
	return room.ScopeWorktree
}

// targetOf defaults the review target to the checkout's current head.
func targetOf(rc *roomContext, given string) string {
	if given != "" {
		return given
	}
	if rc.head != "" {
		return rc.head
	}
	return "working tree"
}
