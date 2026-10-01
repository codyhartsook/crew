// Package notifier is the seam between the harness registry and each harness's own
// way of reaching a live session; the registry stores one Notifier per row.
package notifier

import (
	"context"
	"errors"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// ErrNoNotifier reports a harness with no inbound channel.
var ErrNoNotifier = errors.New("harness has no inbound notification channel")

// ErrSessionGone reports an unresolvable target, so the session should be
// retired. Needed because daemon-hosted Codex sessions share a pid.
var ErrSessionGone = errors.New("session is gone")

// Notifier delivers text to a live session as a new user turn.
type Notifier func(ctx context.Context, s *session.Session, text string) error
