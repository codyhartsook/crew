package harness

import (
	"context"
	"fmt"

	"github.com/codyhartsook/multiplayer/internal/harness/waker"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// CanWake reports whether a harness has an inbound channel.
func CanWake(h session.Harness) bool {
	spec, ok := For(h)
	return ok && spec.Wake != nil
}

// WakeSession delivers text through s's harness waker. ErrNoWaker means the
// room stays the durable path, not that anything failed.
func WakeSession(ctx context.Context, s *session.Session, text string) error {
	spec, ok := For(s.Harness)
	if !ok || spec.Wake == nil {
		return waker.ErrNoWaker
	}
	if !s.Active() {
		return fmt.Errorf("wake %s: session is not active", s.Key())
	}
	return spec.Wake(ctx, s, text)
}
