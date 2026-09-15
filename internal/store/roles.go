package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/role"
)

// RoleStore persists which roles a room has activated.
type RoleStore interface {
	// ActivateRole enables a role; activating twice keeps the first time.
	ActivateRole(ctx context.Context, a *role.Activation) error

	// DeactivateRole disables a role; not an error if it was never active.
	DeactivateRole(ctx context.Context, roomKey, roleName string) error

	// ActiveRoles lists a room's active roles, alphabetically by name.
	ActiveRoles(ctx context.Context, roomKey string) ([]*role.Activation, error)
}
