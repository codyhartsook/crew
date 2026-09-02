package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/harness/waker"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// controlSocketDir is where Claude Code opens its per-session control socket,
// named for the session's pid.
const controlSocketDir = "/tmp/cc-socks"

// relayModel keeps the spawned relay cheap; it makes one mechanical call.
const relayModel = "haiku"

// Wake reaches a Claude session, preferring its channel and falling back to a
// print-mode relay. A missing control socket means the session is gone.
func Wake(ctx context.Context, s *session.Session, text string) error {
	if s.PID == 0 {
		return fmt.Errorf("wake claude %s: no pid recorded", s.ID)
	}
	sock := filepath.Join(controlSocketDir, fmt.Sprintf("%d.sock", s.PID))
	if _, err := os.Stat(sock); err != nil {
		return fmt.Errorf("wake claude %s: %w: no socket at %s", s.ID, waker.ErrSessionGone, sock)
	}
	// A channel exists only for a session that opted in, and costs no process
	// and no model turn, so try it before spawning anything.
	if err := Push(s.PID, text); err == nil {
		return nil
	}
	return relayThroughPrint(ctx, s, sock, text)
}

// relayThroughPrint spawns a print-mode process to message the session. Needed
// because the control socket wants a token only that session's process holds.
func relayThroughPrint(ctx context.Context, s *session.Session, sock, text string) error {
	prompt := fmt.Sprintf("Use SendMessage with to set to exactly %q and message set to exactly %q. Do nothing else.",
		"uds:"+sock, text)
	out, err := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--allowedTools", "SendMessage", "--model", relayModel).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wake claude %s: %w: %s", s.ID, err, strings.TrimSpace(string(out)))
	}
	return nil
}
