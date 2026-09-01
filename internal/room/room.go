// Package room models the shared context agents exchange while working in the
// same place: a working tree or a repository, keyed by its path. Rooms are the
// engineer's own process, never committed to the repository.
//
// Entries are append-only. Resolution posts an entry pointing at what it
// resolves, so concurrent agents never contend for a row.
package room

import (
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// Kind is the sort of thing an entry records.
type Kind string

const (
	// KindDecision is a choice made and the reasoning behind it.
	KindDecision Kind = "decision"
	// KindFinding is something learned about the code or the system.
	KindFinding Kind = "finding"
	// KindQuestion asks the room something and stays open until answered.
	KindQuestion Kind = "question"
	// KindHandoff passes work and its state to whoever picks it up next.
	KindHandoff Kind = "handoff"
	// KindReview is a critique of work in this room.
	KindReview Kind = "review"
)

// Kinds lists every valid kind, in the order a briefing presents them.
var Kinds = []Kind{KindDecision, KindFinding, KindQuestion, KindHandoff, KindReview}

func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Addressed reports whether this kind is pushed to other members at their next
// turn. Decisions and findings are reference material; the rest are directed at
// somebody and stay open until resolved.
func (k Kind) Addressed() bool {
	switch k {
	case KindQuestion, KindHandoff, KindReview:
		return true
	}
	return false
}

// Scope distinguishes the two levels of room.
type Scope string

const (
	// ScopeWorktree is the working tree a session sits in. A pooled treehouse
	// slot has its own room, separate from the repository's main checkout.
	ScopeWorktree Scope = "worktree"
	// ScopeRepo is the repository that owns the working tree. Entries here
	// outlive any single worktree, which matters when a pooled one is returned.
	ScopeRepo Scope = "repo"
)

type Room struct {
	Key   string `json:"key"`
	Scope Scope  `json:"scope"`
	Name  string `json:"name"`
}

type Entry struct {
	ID     int64  `json:"id"`
	Room   string `json:"room"`
	Scope  Scope  `json:"scope"`
	Kind   Kind   `json:"kind"`
	Author string `json:"author"`
	Body   string `json:"body"`

	// ReviewID groups a finding into the review batch it came from; zero for a
	// standalone entry.
	ReviewID int64 `json:"review_id,omitempty"`
	// Anchor and Severity are set on review findings.
	Anchor   *Anchor  `json:"anchor,omitempty"`
	Severity Severity `json:"severity,omitempty"`

	// Resolves is the entry this one answers or closes; zero when it opens
	// rather than closes something.
	Resolves int64 `json:"resolves,omitempty"`
	// ResolvedBy is filled in on read with the entry that closed this one.
	ResolvedBy int64 `json:"resolved_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// Open reports whether the entry still wants an answer. Only addressed kinds
// can be open, and only while nothing has resolved them.
func (e *Entry) Open() bool {
	return e.Kind.Addressed() && e.Resolves == 0 && e.ResolvedBy == 0
}

// Membership records that a session is listening to a room.
type Membership struct {
	SessionKey string    `json:"session_key"`
	Room       string    `json:"room"`
	Scope      Scope     `json:"scope"`
	JoinedAt   time.Time `json:"joined_at"`
}

// Filter narrows a listing. Zero-valued fields do not constrain the result.
type Filter struct {
	IDs []int64
	// ReviewID restricts results to one review's findings.
	ReviewID int64
	Rooms    []string
	Kinds    []Kind
	OpenOnly bool
	Limit    int
}

// For returns the rooms a location belongs to: its working tree and the
// repository that owns it. In a primary checkout the two are one path, so a
// single room comes back and nothing is said twice. Outside a checkout the
// working directory is its own room.
func For(repo *session.Repo, th *session.Treehouse, cwd string) []Room {
	if repo == nil {
		if cwd == "" {
			return nil
		}
		return []Room{{Key: cwd, Scope: ScopeWorktree, Name: baseName(cwd)}}
	}

	rooms := []Room{{Key: repo.Root, Scope: ScopeWorktree, Name: worktreeName(repo, th)}}
	if repo.MainRoot != "" && repo.MainRoot != repo.Root {
		rooms = append(rooms, Room{Key: repo.MainRoot, Scope: ScopeRepo, Name: repo.Name})
	}
	return rooms
}

// Keys is what the store indexes on.
func Keys(rooms []Room) []string {
	keys := make([]string, 0, len(rooms))
	for _, r := range rooms {
		keys = append(keys, r.Key)
	}
	return keys
}

// worktreeName labels a working tree distinctly from its repository. A pooled
// worktree is usually a directory named after the repo inside a numbered slot,
// so naming it after its own leaf gives "widget/widget"; the slot is what
// actually tells two of them apart.
func worktreeName(repo *session.Repo, th *session.Treehouse) string {
	if !repo.IsWorktree {
		return repo.Name
	}
	if th != nil && th.Slot != "" {
		return repo.Name + "/" + th.Slot
	}
	leaf := baseName(repo.Root)
	if leaf == repo.Name {
		if parent := baseName(parentDir(repo.Root)); parent != "" && parent != leaf {
			leaf = parent
		}
	}
	return repo.Name + "/" + leaf
}

func parentDir(path string) string {
	for i := len(path) - 1; i > 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return ""
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
