// Package session defines the domain model shared by the store, API, CLI and
// hooks: one coding-agent process and wherever it is working.
package session

import "time"

// Harness identifies the coding agent CLI that opened a session.
type Harness string

const (
	HarnessClaude  Harness = "claude"
	HarnessCodex   Harness = "codex"
	HarnessUnknown Harness = "unknown"
)

func (h Harness) Valid() bool {
	switch h {
	case HarnessClaude, HarnessCodex, HarnessUnknown:
		return true
	}
	return false
}

// Status is the lifecycle state of a session.
type Status string

const (
	// StatusActive means the harness reported a session start and has not reported an end.
	StatusActive Status = "active"
	// StatusEnded means the harness reported a session end.
	StatusEnded Status = "ended"
)

// Session is one agent CLI process and the place it is working.
//
// ID is the harness-assigned session id. It is unique per harness but not
// guaranteed unique across harnesses, so stores key on the (Harness, ID) pair
// projected into Key.
type Session struct {
	ID      string  `json:"id"`
	Harness Harness `json:"harness"`
	Status  Status  `json:"status"`

	PID  int    `json:"pid,omitempty"`
	Host string `json:"host,omitempty"`
	User string `json:"user,omitempty"`
	CWD  string `json:"cwd"`

	// Repo is nil when the session opened outside any git checkout.
	Repo *Repo `json:"repo,omitempty"`
	// Treehouse is nil unless Repo points at a pooled treehouse worktree.
	Treehouse *Treehouse `json:"treehouse,omitempty"`

	StartedAt time.Time  `json:"started_at"`
	LastSeen  time.Time  `json:"last_seen"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	EndReason string     `json:"end_reason,omitempty"`

	// Meta carries harness-specific extras (model, permission mode, source, ...).
	Meta map[string]string `json:"meta,omitempty"`
}

// Key is the store-wide unique identity of a session.
func (s *Session) Key() string { return string(s.Harness) + ":" + s.ID }

// Active reports whether the session is still believed to be running.
func (s *Session) Active() bool { return s.Status == StatusActive }

// Repo is the git checkout a session is working in.
//
// Root is the working tree the session sits in. MainRoot is the primary
// checkout that owns the shared .git directory; the two differ exactly when
// Root is a linked worktree.
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

// Treehouse describes a worktree drawn from a treehouse pool.
//
// Pool is the pool directory name (repo plus a hash of its origin), Slot is the
// numbered worktree within it. Leased is what treehouse itself recorded, which
// is independent of whether an agent session is currently registered here.
type Treehouse struct {
	Pool        string `json:"pool"`
	Slot        string `json:"slot"`
	Root        string `json:"root"`
	Leased      bool   `json:"leased"`
	LeaseID     string `json:"lease_id,omitempty"`
	LeaseHolder string `json:"lease_holder,omitempty"`
}
