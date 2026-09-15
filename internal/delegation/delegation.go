// Package delegation is a delegated task: what was asked, of which role, and
// what came back. Fire-and-forget is the default; --wait just blocks the
// launcher on the same record instead of returning immediately.
package delegation

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

// Delegation is one call to a role.
type Delegation struct {
	ID   string `json:"id"`
	Room string `json:"room"`
	// Dir is the filesystem directory to spawn and resolve roles in. Not
	// always Room: a pooled worktree's room key has a "#<lease>" suffix that
	// is not a valid path.
	Dir       string    `json:"dir"`
	Role      string    `json:"role"`
	Harness   string    `json:"harness"`
	Requester string    `json:"requester"`
	Prompt    string    `json:"prompt"`
	Status    Status    `json:"status"`
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	Notified  bool      `json:"notified"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Notice is the one-line nudge for finished delegations: enough to know
// something is waiting, not the result itself.
func Notice(ds []*Delegation) string {
	if len(ds) == 0 {
		return ""
	}
	ids := make([]string, len(ds))
	for i, d := range ds {
		ids[i] = d.ID
	}
	word := "delegation"
	if len(ds) != 1 {
		word = "delegations"
	}
	return fmt.Sprintf("crew: %d %s finished (%s); run `crew delegate result <id>` to read.",
		len(ds), word, strings.Join(ids, ", "))
}

// Filter narrows a listing. Zero-valued fields do not constrain the result.
type Filter struct {
	Requester  string
	Status     Status
	Unnotified bool
	Limit      int
}
