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

// Mode determines whether an entry is reference material or a request.
type Mode string

const (
	// ModeNote is durable context for everyone in the room.
	ModeNote Mode = "note"
	// ModeRequest asks the room to act and stays open until resolved.
	ModeRequest Mode = "request"
)

// Modes lists every valid mode, in display order.
var Modes = []Mode{ModeNote, ModeRequest}

func (m Mode) Valid() bool {
	for _, known := range Modes {
		if m == known {
			return true
		}
	}
	return false
}

// Addressed reports whether this mode is pushed to other members.
func (m Mode) Addressed() bool {
	return m == ModeRequest
}

// Scope distinguishes the two levels of room.
type Scope string

const (
	// ScopeWorktree is the working tree a session sits in. A pooled slot has its
	// own room, separate from the repository's main checkout.
	ScopeWorktree Scope = "worktree"
	// ScopeRepo is the repository that owns the working tree. Entries here
	// outlive any single worktree and any single lease of one, which is where
	// anything durable belongs.
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
	Mode   Mode   `json:"mode"`
	Author string `json:"author"`
	To     string `json:"to,omitempty"`
	Body   string `json:"body"`

	// Resolves is the entry this one answers or closes; zero when it opens
	// rather than closes something.
	Resolves int64 `json:"resolves,omitempty"`
	// ResolvedBy is filled in on read with the entry that closed this one.
	ResolvedBy int64 `json:"resolved_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// Open reports whether the entry still wants an answer. Only requests
// can be open, and only while nothing has resolved them.
func (e *Entry) Open() bool {
	return e.Mode.Addressed() && e.Resolves == 0 && e.ResolvedBy == 0
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
	IDs      []int64
	Rooms    []string
	Modes    []Mode
	OpenOnly bool
	Limit    int
}

// Query is a free-text search over room entries.
type Query struct {
	Text  string
	Rooms []string
	Limit int
}

// For returns the rooms a location belongs to: its working tree and the
// repository that owns it. In a primary checkout the two are one path, so a
// single room comes back and nothing is said twice. Outside a checkout the
// working directory is its own room.
func For(repo *session.Repo, pool *session.Pool, cwd string) []Room {
	if repo == nil {
		if cwd == "" {
			return nil
		}
		return []Room{{Key: cwd, Scope: ScopeWorktree, Name: baseName(cwd)}}
	}

	rooms := []Room{{Key: worktreeKey(repo, pool), Scope: ScopeWorktree, Name: worktreeName(repo, pool)}}
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

// worktreeKey identifies the working-tree room. A pooled slot's path is handed
// to the next lease, so the lease is part of the room's identity: keyed on the
// path alone, a returned slot gives the next feature the previous room context.
func worktreeKey(repo *session.Repo, pool *session.Pool) string {
	if pool != nil && pool.LeaseID != "" {
		return repo.Root + "#" + pool.LeaseID
	}
	return repo.Root
}

// worktreeName labels a working tree distinctly from its repository. A pooled
// worktree is usually a directory named after the repo inside a numbered slot,
// so naming it after its own leaf gives "widget/widget"; the slot is what
// actually tells two of them apart.
func worktreeName(repo *session.Repo, pool *session.Pool) string {
	if !repo.IsWorktree {
		return repo.Name
	}
	if pool != nil && pool.Slot != "" {
		return repo.Name + "/" + pool.Slot
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
