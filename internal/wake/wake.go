// Package wake delivers notifications into live agent sessions and retires the
// ones their harness reports gone.
package wake

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/waker"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// Ender retires a session. store.Store satisfies it.
type Ender interface {
	End(ctx context.Context, key string, at time.Time, reason string) error
}

// wakeFunc is the harness call, injected so tests do not shell out.
type wakeFunc func(ctx context.Context, s *session.Session, text string) error

// Deliver wakes s, retiring it when the harness reports it gone. That retirement
// is the only liveness signal a daemon-hosted session has, since it shares the
// daemon's pid and so survives prune.
//
// The waker error is returned unchanged, so a caller can read ErrNoWaker as
// "the room is still the durable path" rather than as a failure.
func Deliver(ctx context.Context, e Ender, s *session.Session, text string) error {
	return deliver(ctx, e, harness.WakeSession, s, text)
}

func deliver(ctx context.Context, e Ender, wake wakeFunc, s *session.Session, text string) error {
	err := wake(ctx, s, text)
	if !errors.Is(err, waker.ErrSessionGone) {
		return err
	}
	if endErr := e.End(ctx, s.Key(), time.Now().UTC(), "wake found the session gone"); endErr != nil && !errors.Is(endErr, store.ErrNotFound) {
		return errors.Join(err, fmt.Errorf("retire %s: %w", s.Key(), endErr))
	}
	return err
}
