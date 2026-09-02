// Package waker is the seam between the harness registry and each harness's
// own way of reaching a live session. A harness package implements a Waker;
// the registry stores one per row.
package waker

import (
	"context"
	"errors"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// ErrNoWaker reports a harness with no inbound channel.
var ErrNoWaker = errors.New("harness has no inbound wake channel")

// ErrSessionGone reports an unresolvable target, so the session should be
// retired. Needed because daemon-hosted Codex sessions share a pid.
var ErrSessionGone = errors.New("session is gone")

// Waker delivers text to a live session as a new user turn.
type Waker func(ctx context.Context, s *session.Session, text string) error
