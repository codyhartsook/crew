package harness_test

import (
	"context"
	"os"
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
