package harness_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/session"
)

// TestLiveWakeCodex wakes a real Codex thread. It is skipped unless
// LIVE_CODEX_THREAD names one, because it injects a turn into that session.
func TestLiveWakeCodex(t *testing.T) {
	thread := os.Getenv("LIVE_CODEX_THREAD")
	if thread == "" {
		t.Skip("set LIVE_CODEX_THREAD to a live Codex thread id")
	}
	s := &session.Session{ID: thread, Harness: session.HarnessCodex, Status: session.StatusActive}
	if err := harness.WakeSession(context.Background(), s, os.Getenv("LIVE_CODEX_MESSAGE")); err != nil {
		t.Fatalf("wake %s: %v", thread, err)
	}
	t.Logf("wake queued for %s", thread)
}

// TestLiveWakeClaude wakes a real Claude session, named by the pid its control
// socket is named for. Skipped unless LIVE_CLAUDE_PID is set, because it
// spawns a print-mode process and injects a turn into that session.
func TestLiveWakeClaude(t *testing.T) {
	pid, _ := strconv.Atoi(os.Getenv("LIVE_CLAUDE_PID"))
	if pid == 0 {
		t.Skip("set LIVE_CLAUDE_PID to a live Claude session's pid")
	}
	s := &session.Session{ID: "live", Harness: session.HarnessClaude, Status: session.StatusActive, PID: pid}
	if err := harness.WakeSession(context.Background(), s, os.Getenv("LIVE_CLAUDE_MESSAGE")); err != nil {
		t.Fatalf("wake pid %d: %v", pid, err)
	}
	t.Logf("wake sent to pid %d", pid)
}
