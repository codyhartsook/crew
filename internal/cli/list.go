package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func newListCmd(opts *options) *cobra.Command {
	var (
		filter    store.Filter
		harness   string
		status    string
		all       bool
		asJSON    bool
		treehouse bool
	)

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List agent sessions and where they are working",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := opts.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			filter.Harness = session.Harness(harness)
			filter.TreehouseOnly = treehouse
			// Live sessions are what you almost always want; --all or an
			// explicit --status opens it up to finished ones.
			switch {
			case status != "":
				filter.Status = session.Status(status)
			case !all:
				filter.Status = session.StatusActive
			}

			sessions, err := st.List(cmd.Context(), filter)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSONList(cmd.OutOrStdout(), sessions)
			}
			return writeTable(cmd.OutOrStdout(), sessions)
		},
	}

	cmd.Flags().StringVar(&harness, "harness", "", "filter by harness (claude, codex)")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (active, ended)")
	cmd.Flags().StringVar(&filter.RepoName, "repo", "", "filter by repository")
	cmd.Flags().StringVar(&filter.RepoRoot, "repo-root", "", "filter by exact working tree")
	cmd.Flags().BoolVar(&treehouse, "treehouse", false, "only treehouse pool worktrees")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include sessions that have ended")
	cmd.Flags().IntVar(&filter.Limit, "limit", 0, "max sessions to show")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}

func newGetCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "get <harness:session-id>",
		Short: "Show one session as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := opts.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			sess, err := st.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(sess)
		},
	}
}

func newRemoveCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <harness:session-id>...",
		Aliases: []string{"remove"},
		Short:   "Delete session records",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := opts.openStore()
			if err != nil {
				return err
			}
			defer st.Close()

			for _, key := range args {
				if err := st.Delete(cmd.Context(), key); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "deleted", key)
			}
			return nil
		},
	}
}

func writeJSONList(w io.Writer, sessions []*session.Session) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(sessions)
}

func writeTable(w io.Writer, sessions []*session.Session) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(w, "no sessions")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HARNESS\tSESSION\tREPO\tBRANCH\tWORKTREE\tSTATUS\tLAST SEEN")
	for _, s := range sessions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			dash(string(s.Harness)),
			shortID(s.ID),
			repoName(s),
			branchOf(s),
			worktreeOf(s),
			dash(string(s.Status)),
			age(s.LastSeen),
		)
	}
	return tw.Flush()
}

func repoName(s *session.Session) string {
	if s.Repo == nil {
		return "-"
	}
	return dash(s.Repo.Name)
}

func branchOf(s *session.Session) string {
	if s.Repo == nil {
		return "-"
	}
	if s.Repo.Branch != "" {
		return s.Repo.Branch
	}
	if s.Repo.Detached {
		return "detached"
	}
	return "-"
}

// worktreeOf names the checkout: the pool slot for a treehouse worktree, the
// directory name for any other linked worktree, and "main" for the primary
// checkout.
func worktreeOf(s *session.Session) string {
	if s.Treehouse != nil {
		return s.Treehouse.Pool + "/" + s.Treehouse.Slot
	}
	if s.Repo == nil {
		return "-"
	}
	if !s.Repo.IsWorktree {
		return "main"
	}
	return strings.TrimPrefix(s.Repo.Root, s.Repo.MainRoot+"/")
}

// shortID trims a session id to a prefix long enough to stay unique in practice
// while keeping the table narrow. The full id is in --json and get.
func shortID(id string) string {
	const width = 8
	if len(id) <= width {
		return id
	}
	return id[:width] + "…"
}

// age renders how long ago t was, in the largest unit that stays readable.
func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
