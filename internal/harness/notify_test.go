package harness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/notifier"
	"github.com/codyhartsook/multiplayer/internal/session"
)

func TestNotifySessionWithoutNotifier(t *testing.T) {
	for _, h := range []session.Harness{session.HarnessUnknown, session.Harness("nope")} {
		s := &session.Session{ID: "x", Harness: h, Status: session.StatusActive}
		if err := harness.NotifySession(context.Background(), s, "hi"); !errors.Is(err, notifier.ErrNoNotifier) {
			t.Errorf("%s: got %v, want ErrNoNotifier", h, err)
		}
	}
}

func TestClaudeHasNotifier(t *testing.T) {
	spec, ok := harness.For(session.HarnessClaude)
	if !ok || spec.Notify == nil {
		t.Fatal("claude spec has no notifier")
	}
}

// A claude wake resolves its target from the recorded pid, so both checks below
// fail before anything is spawned.
func TestWakeClaudeNeedsAPID(t *testing.T) {
	s := &session.Session{ID: "c1", Harness: session.HarnessClaude, Status: session.StatusActive}
	err := harness.NotifySession(context.Background(), s, "hi")
	if err == nil || errors.Is(err, notifier.ErrSessionGone) {
		t.Fatalf("got %v, want a missing-pid error", err)
	}
}

func TestWakeClaudeWithoutSocketIsGone(t *testing.T) {
	s := &session.Session{ID: "c1", Harness: session.HarnessClaude, Status: session.StatusActive, PID: 2147483000}
	if err := harness.NotifySession(context.Background(), s, "hi"); !errors.Is(err, notifier.ErrSessionGone) {
		t.Fatalf("got %v, want ErrSessionGone", err)
	}
}

func TestNotifySessionRejectsEnded(t *testing.T) {
	s := &session.Session{ID: "x", Harness: session.HarnessCodex, Status: session.StatusEnded}
	err := harness.NotifySession(context.Background(), s, "hi")
	if err == nil || errors.Is(err, notifier.ErrNoNotifier) {
		t.Fatalf("got %v, want a not-active error", err)
	}
}

func TestCodexHasNotifier(t *testing.T) {
	spec, ok := harness.For(session.HarnessCodex)
	if !ok || spec.Notify == nil {
		t.Fatal("codex spec has no notifier")
	}
}
