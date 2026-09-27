// Package lscmd lists agent sessions and where they are working.
package lscmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codyhartsook/multiplayer/internal/cli/cmdutil"
	"github.com/codyhartsook/multiplayer/internal/cli/roomctx"
	"github.com/codyhartsook/multiplayer/internal/cli/view"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

func New(opts *cmdutil.Options) *cobra.Command {
	var (
		filter  store.Filter
		harness string
		status  string
		all     bool
		asJSON  bool
		pooled  bool
	)

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List agent sessions and where they are working",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := opts.OpenStore()
			if err != nil {
				return err
			}
			defer st.Close()

			filter.Harness = session.Harness(harness)
			filter.PooledOnly = pooled
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
			roles := map[string]*store.Role{}
			if rs, ok := st.(store.RoleStore); ok {
				held, err := rs.Roles(cmd.Context(), store.RoleFilter{})
				if err != nil {
					return err
				}
				for _, r := range held {
					roles[r.SessionKey] = r
				}
			}
			if asJSON {
				rows := make([]jsonRow, 0, len(sessions))
				for _, s := range sessions {
					rows = append(rows, jsonRow{s, roles[s.Key()]})
				}
				return cmdutil.WriteJSON(cmd.OutOrStdout(), rows)
			}
			self := roomctx.Self(cmd.Context(), st, roomctx.Cwd())
			return writeTable(cmd.OutOrStdout(), opts.Human, sessions, roles, self)
		},
	}

	cmd.Flags().StringVar(&harness, "harness", "", "filter by harness (claude, codex)")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (active, ended)")
	cmd.Flags().StringVar(&filter.RepoName, "repo", "", "filter by repository")
	cmd.Flags().StringVar(&filter.RepoRoot, "repo-root", "", "filter by exact working tree")
	cmd.Flags().BoolVar(&pooled, "pooled", false, "only worktrees lent out by a pool manager")
	cmd.Flags().BoolVar(&pooled, "treehouse", false, "only treehouse pool worktrees")
	_ = cmd.Flags().MarkDeprecated("treehouse", "use --pooled")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include sessions that have ended")
	cmd.Flags().IntVar(&filter.Limit, "limit", 0, "max sessions to show")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}

// jsonRow is a session with the role it holds, if any.
type jsonRow struct {
	*session.Session
	Role *store.Role `json:"role,omitempty"`
}

// sessionColumns is what ls shows. An agent addresses agents by alias, so the
// session id is a person's column, and the default filter is already
// active-only, so is the status.
var sessionColumns = []view.Column{
	{Name: "AGENT"},
	{Name: "HARNESS"},
	{Name: "WHERE"},
	{Name: "ROLE"},
	{Name: "FREE"},
	{Name: "SEEN"},
	{Name: "SESSION", Human: true},
	{Name: "WORKTREE", Human: true},
	{Name: "STATUS", Human: true},
	{Name: "DESCRIPTION", Human: true},
}

// writeTable marks self's row, so an agent can tell which one it is.
func writeTable(w io.Writer, human bool, sessions []*session.Session, roles map[string]*store.Role, self string) error {
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(w, "no sessions")
		return err
	}
	rows := make([][]string, 0, len(sessions))
	for _, s := range sessions {
		name := room.Display(s)
		if s.Key() == self {
			name += " (you)"
		}
		role, desc := "-", "-"
		if r := roles[s.Key()]; r != nil {
			role, desc = r.Name, r.Description
		}
		rows = append(rows, []string{
			name,
			dash(string(s.Harness)),
			where(s),
			role,
			freeContext(s),
			age(s.LastSeen),
			shortID(s.ID),
			worktreeOf(s),
			dash(string(s.Status)),
			desc,
		})
	}
	return view.Table(w, human, sessionColumns, rows)
}

// where names the checkout as repo/branch, which is what an agent needs to
// know what someone is working on.
func where(s *session.Session) string {
	if s.Repo == nil {
		if s.Folder != nil {
			return s.Folder.Name
		}
		return "-"
	}
	name := dash(s.Repo.Name)
	if b := branchOf(s); b != "-" {
		return name + "/" + b
	}
	return name
}

// freeContext is how much of the model's context window is still available,
// or "-" when the harness has not reported it.
func freeContext(s *session.Session) string {
	if s.Usage == nil {
		return "-"
	}
	free, ok := s.Usage.Headroom()
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", free*100)
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

// worktreeOf names the checkout: the pool slot for a lent-out worktree, the
// directory name for any other linked worktree, and "main" for the primary
// checkout.
func worktreeOf(s *session.Session) string {
	if s.Pool != nil {
		return s.Pool.Name + "/" + s.Pool.Slot
	}
	if s.Repo == nil {
		if s.Folder != nil {
			return "folder"
		}
		return "-"
	}
	if !s.Repo.IsWorktree {
		return "main"
	}
	return strings.TrimPrefix(s.Repo.Root, s.Repo.MainRoot+"/")
}

// shortID trims a session id to a prefix long enough to stay unique in practice
// while keeping the table narrow. The full id is in --json.
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
