package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/routing"
)

// RoutingStore records tier 1 routing decisions: which roster a session saw
// and when, so a later pass can tell a useful rule from an annoying one.
type RoutingStore interface {
	// LogRouting records that a roster was shown. Assigns the decision's ID.
	LogRouting(ctx context.Context, d *routing.Decision) error

	// RoutingLog lists decisions for a room, oldest first.
	RoutingLog(ctx context.Context, roomKey string) ([]*routing.Decision, error)
}
