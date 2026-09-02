// Package codex holds what is specific to the Codex CLI.
package codex

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/harness/waker"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// gone are queue failures meaning the thread no longer resolves. Nothing else
// is classified: a routing error must not retire a live session.
var gone = []string{
	"no rollout found for thread id",
	"no rollout found for conversation id",
}

// Wake queues a turn on a live Codex thread; the session id is the thread id.
// An interrupted thread leaves the queue paused and drops the wake.
func Wake(ctx context.Context, s *session.Session, text string) error {
	if s.ID == "" {
		return errors.New("wake codex: empty thread id")
	}
	out, err := exec.CommandContext(ctx, "codex", "queue", "--thread", s.ID, "--message", text).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	for _, pat := range gone {
		if strings.Contains(msg, pat) {
			return fmt.Errorf("wake codex %s: %w: %s", s.ID, waker.ErrSessionGone, msg)
		}
	}
	return fmt.Errorf("wake codex %s: %w: %s", s.ID, err, msg)
}
