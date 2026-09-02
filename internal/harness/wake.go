package harness

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// ErrNoWaker reports that a harness has no inbound channel, so the session can
// only be reached through hook-time reconciliation.
var ErrNoWaker = errors.New("harness has no inbound wake channel")

// ErrSessionGone reports that the harness could not resolve the target, so the
// session is not live and should be retired. It matters because a daemon-hosted
// Codex session shares the daemon's pid, so process liveness cannot detect one
// whose own process is gone.
var ErrSessionGone = errors.New("session is gone")

// Delivery is when a woken session acts on the message.
type Delivery string

const (
	// DeliverOnIdle queues the message and runs it when the session next goes
	// idle. Sending to a busy session is safe.
	DeliverOnIdle Delivery = "on-idle"
	// DeliverImmediate reaches the session at its next tool round.
	DeliverImmediate Delivery = "immediate"
)

// Waker delivers text to a live session as a new user turn.
type Waker func(ctx context.Context, s *session.Session, text string) error

// WakeSession delivers text to s through its harness waker. It returns
// ErrNoWaker when the harness has none, which callers treat as "the room is
// still the durable path", not as a failure.
func WakeSession(ctx context.Context, s *session.Session, text string) error {
	spec, ok := For(s.Harness)
	if !ok || spec.Wake == nil {
		return ErrNoWaker
	}
	if !s.Active() {
		return fmt.Errorf("wake %s: session is not active", s.Key())
	}
	return spec.Wake(ctx, s, text)
}

// codexGone are the queue failures meaning the thread no longer resolves.
// Nothing else is classified: a capability or routing error must not retire a
// session that is still live.
var codexGone = []string{
	"no rollout found for thread id",
	"no rollout found for conversation id",
}

// wakeCodex queues a turn on a live Codex thread. The registry session id is
// the Codex thread id, so this needs no lookup.
//
// One limit is the harness's, not ours: a thread whose last turn was
// interrupted leaves the queue paused and silently drops the wake. A session
// that has taken no turns is fine, and the wake starts its first turn.
func wakeCodex(ctx context.Context, s *session.Session, text string) error {
	if s.ID == "" {
		return errors.New("wake codex: empty thread id")
	}
	out, err := exec.CommandContext(ctx, "codex", "queue", "--thread", s.ID, "--message", text).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	for _, pat := range codexGone {
		if strings.Contains(msg, pat) {
			return fmt.Errorf("wake codex %s: %w: %s", s.ID, ErrSessionGone, msg)
		}
	}
	return fmt.Errorf("wake codex %s: %w: %s", s.ID, err, msg)
}
