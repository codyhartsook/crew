package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Role is a role an agent took on in a room. It lasts as long as its session.
type Role struct {
	SessionKey  string    `json:"session_key"`
	Room        string    `json:"room"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	AssignedAt  time.Time `json:"assigned_at"`
}

// Limits on a role, kept small because every session pays for its skill listing.
const (
	MaxRoleName        = 32
	MaxRoleDescription = 300
)

var roleName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate checks a role before it is stored.
func (r *Role) Validate() error {
	if !roleName.MatchString(r.Name) || len(r.Name) > MaxRoleName {
		return fmt.Errorf("role name %q must be lowercase letters, digits and dashes, start with a letter, and be at most %d characters", r.Name, MaxRoleName)
	}
	switch {
	case strings.TrimSpace(r.Description) == "":
		return errors.New("a role needs a description: say which tasks other agents should send you")
	case strings.ContainsAny(r.Description, "\r\n"):
		return errors.New("the role description must be one line")
	case utf8.RuneCountInString(r.Description) > MaxRoleDescription:
		return fmt.Errorf("the role description is %d characters; keep it to %d", utf8.RuneCountInString(r.Description), MaxRoleDescription)
	}
	return nil
}

// RoleFilter narrows a Roles call. Zero-valued fields do not constrain it.
type RoleFilter struct {
	Rooms []string
	// ActiveOnly keeps roles whose session is still active.
	ActiveOnly bool
}

// RoleStore persists role assignments, one per session.
type RoleStore interface {
	// Assign records a session's role, replacing any it already held.
	Assign(ctx context.Context, r *Role) error

	// Drop removes a session's role and reports whether it had one.
	Drop(ctx context.Context, sessionKey string) (bool, error)

	// Roles lists assignments matching f, oldest first.
	Roles(ctx context.Context, f RoleFilter) ([]*Role, error)
}
