// Package notify delivers notifications into live agent sessions and retires the
// ones their harness reports gone.
package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/notifier"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// Ender retires a session. store.Store satisfies it.
type Ender interface {
	End(ctx context.Context, key string, at time.Time, reason string) error
}

// notifyFunc is the harness call, injected so tests do not shell out.
type notifyFunc func(ctx context.Context, s *session.Session, text string) error

// Deliver wakes s, retiring it when the harness reports it gone. That retirement
// is the only liveness signal a daemon-hosted session has, since it shares the
// daemon's pid and so survives prune.
//
// The notifier error is returned unchanged, so a caller can read ErrNoNotifier as
// "the room is still the durable path" rather than as a failure.
func Deliver(ctx context.Context, e Ender, s *session.Session, text string) error {
	return deliver(ctx, e, harness.NotifySession, s, text)
}

func deliver(ctx context.Context, e Ender, notify notifyFunc, s *session.Session, text string) error {
	err := notify(ctx, s, text)
	if !errors.Is(err, notifier.ErrSessionGone) {
		return err
	}
	if endErr := e.End(ctx, s.Key(), time.Now().UTC(), "wake found the session gone"); endErr != nil && !errors.Is(endErr, store.ErrNotFound) {
		return errors.Join(err, fmt.Errorf("retire %s: %w", s.Key(), endErr))
	}
	return err
}
