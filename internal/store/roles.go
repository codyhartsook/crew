package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/role"
)

// RoleStore persists which roles a room has activated, separate from
// RoomStore because activation is bookkeeping about roles, not room content.
type RoleStore interface {
	// ActivateRole enables a role in a room. Activating an already-active role
	// is a no-op that keeps the original activation time.
	ActivateRole(ctx context.Context, a *role.Activation) error

	// DeactivateRole disables a role in a room. Not an error if it was never
	// active.
	DeactivateRole(ctx context.Context, roomKey, roleName string) error

	// ActiveRoles lists the roles active in a room, alphabetically by name.
	ActiveRoles(ctx context.Context, roomKey string) ([]*role.Activation, error)
}
