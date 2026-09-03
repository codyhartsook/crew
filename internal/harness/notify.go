package harness

import (
	"context"
	"fmt"

	"github.com/codyhartsook/multiplayer/internal/harness/notifier"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// CanNotify reports whether a harness has an inbound channel.
func CanNotify(h session.Harness) bool {
	spec, ok := For(h)
	return ok && spec.Notify != nil
}

// NotifySession delivers text through s's harness notifier. ErrNoNotifier
// means the room stays the durable path, not that anything failed.
func NotifySession(ctx context.Context, s *session.Session, text string) error {
	spec, ok := For(s.Harness)
	if !ok || spec.Notify == nil {
		return notifier.ErrNoNotifier
	}
	if !s.Active() {
		return fmt.Errorf("notify %s: session is not active", s.Key())
	}
	return spec.Notify(ctx, s, text)
}
