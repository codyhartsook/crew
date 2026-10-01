// Package session defines the domain model shared by the store, API, CLI and
// hooks: one coding-agent process and wherever it is working.
package session

import (
	"math/rand/v2"
	"time"

	"github.com/codyhartsook/multiplayer/internal/usage"
)

// Harness identifies the coding agent CLI that opened a session.
type Harness string

const (
	HarnessClaude  Harness = "claude"
	HarnessCodex   Harness = "codex"
	HarnessUnknown Harness = "unknown"
)

func (h Harness) Valid() bool {
	// Any non-empty name persists, as sessions may outlive the binary that knows
	// the harness. The harness registry decides what is available locally.
	return h != ""
}

// Status is the lifecycle state of a session.
type Status string

const (
	// StatusActive means the harness reported a session start and has not reported an end.
	StatusActive Status = "active"
	// StatusEnded means the harness reported a session end.
	StatusEnded Status = "ended"
)

// Session is one agent CLI process and where it works. ID is unique per
// harness only, so stores key on the (Harness, ID) pair projected into Key.
type Session struct {
	ID      string  `json:"id"`
	Harness Harness `json:"harness"`
	// Alias is a short-lived, human-friendly name assigned by the registry.
	// Identity and authorization always use Key.
	Alias  string `json:"alias,omitempty"`
	Status Status `json:"status"`

	PID  int    `json:"pid,omitempty"`
	Host string `json:"host,omitempty"`
	User string `json:"user,omitempty"`

	// Embedded, so the location fields stay flat in JSON and are read
	// directly off a Session.
	Place

	StartedAt time.Time  `json:"started_at"`
	LastSeen  time.Time  `json:"last_seen"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	EndReason string     `json:"end_reason,omitempty"`

	// Meta carries harness-specific extras (model, permission mode, source, ...).
	Meta map[string]string `json:"meta,omitempty"`

	// Usage is what this session has spent, nil until a turn reports it.
	Usage *usage.Snapshot `json:"usage,omitempty"`
}

// Key is the store-wide unique identity of a session.
func (s *Session) Key() string { return string(s.Harness) + ":" + s.ID }

// Active reports whether the session is still believed to be running.
func (s *Session) Active() bool { return s.Status == StatusActive }

var aliasColours = [...]string{
	"amber", "blue", "coral", "dawn", "fern", "gold", "indigo", "jade",
	"lilac", "moss", "pearl", "rose", "rust", "sage", "sky", "violet",
}

var aliasAnimals = [...]string{
	"badger", "beaver", "bison", "crane", "dolphin", "falcon", "gecko", "heron",
	"lynx", "otter", "panda", "raven", "seal", "tiger", "wren", "yak",
}

// RandomAlias picks an unused, human-friendly session alias. There are enough
// combinations for normal local collaboration; callers reject a full pool.
func RandomAlias(used map[string]bool) (string, bool) {
	count := len(aliasColours) * len(aliasAnimals)
	start := rand.IntN(count)
	for i := 0; i < count; i++ {
		n := (start + i) % count
		name := aliasColours[n/len(aliasAnimals)] + "-" + aliasAnimals[n%len(aliasAnimals)]
		if !used[name] {
			return name, true
		}
	}
	return "", false
}

// Place is where a session is working: its directory plus at most one owner,
// either a git checkout or an anchored folder.
type Place struct {
	CWD string `json:"cwd"`
	// Repo is nil when the session opened outside any git checkout.
	Repo *Repo `json:"repo,omitempty"`
	// Pool is nil unless Repo points at a worktree lent out by a pool manager.
	Pool *Pool `json:"pool,omitempty"`
	// Folder is nil unless the session sits in an anchored plain folder.
	Folder *Folder `json:"folder,omitempty"`
}

// Folder is a plain directory a session works in, outside any git checkout.
// Root is the anchor: sessions under it, even in subdirectories, share one room.
type Folder struct {
	Name string `json:"name"`
	Root string `json:"root"`
}

// Repo is the git checkout a session works in. Root is its working tree;
// MainRoot is the primary checkout owning .git, and differs only in a linked worktree.
type Repo struct {
	Name       string `json:"name"`
	Root       string `json:"root"`
	MainRoot   string `json:"main_root,omitempty"`
	Remote     string `json:"remote,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Head       string `json:"head,omitempty"`
	Detached   bool   `json:"detached,omitempty"`
	IsWorktree bool   `json:"is_worktree"`
}

// Pool is a worktree manager that lends out checkouts, and the one it lent here.
// Leased is what the manager recorded, regardless of any registered session.
type Pool struct {
	Manager     string `json:"manager"`
	Name        string `json:"name"`
	Slot        string `json:"slot"`
	Root        string `json:"root"`
	Leased      bool   `json:"leased"`
	LeaseID     string `json:"lease_id,omitempty"`
	LeaseHolder string `json:"lease_holder,omitempty"`
}
