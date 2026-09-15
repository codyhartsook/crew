package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/routing"
)

// RoutingStore records which roster a session saw, and when.
type RoutingStore interface {
	// LogRouting records a shown roster and assigns the decision's ID.
	LogRouting(ctx context.Context, d *routing.Decision) error

	// RoutingLog lists decisions for a room, oldest first.
	RoutingLog(ctx context.Context, roomKey string) ([]*routing.Decision, error)
}
