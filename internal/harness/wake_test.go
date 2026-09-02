package harness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/harness/waker"
	"github.com/codyhartsook/multiplayer/internal/session"
)

func TestWakeSessionWithoutWaker(t *testing.T) {
	for _, h := range []session.Harness{session.HarnessUnknown, session.Harness("nope")} {
		s := &session.Session{ID: "x", Harness: h, Status: session.StatusActive}
		if err := harness.WakeSession(context.Background(), s, "hi"); !errors.Is(err, waker.ErrNoWaker) {
			t.Errorf("%s: got %v, want ErrNoWaker", h, err)
		}
	}
}

func TestClaudeHasWaker(t *testing.T) {
	spec, ok := harness.For(session.HarnessClaude)
	if !ok || spec.Wake == nil {
		t.Fatal("claude spec has no waker")
	}
}

// A claude wake resolves its target from the recorded pid, so both checks below
// fail before anything is spawned.
func TestWakeClaudeNeedsAPID(t *testing.T) {
	s := &session.Session{ID: "c1", Harness: session.HarnessClaude, Status: session.StatusActive}
	err := harness.WakeSession(context.Background(), s, "hi")
	if err == nil || errors.Is(err, waker.ErrSessionGone) {
		t.Fatalf("got %v, want a missing-pid error", err)
	}
}

func TestWakeClaudeWithoutSocketIsGone(t *testing.T) {
	s := &session.Session{ID: "c1", Harness: session.HarnessClaude, Status: session.StatusActive, PID: 2147483000}
	if err := harness.WakeSession(context.Background(), s, "hi"); !errors.Is(err, waker.ErrSessionGone) {
		t.Fatalf("got %v, want ErrSessionGone", err)
	}
}

func TestWakeSessionRejectsEnded(t *testing.T) {
	s := &session.Session{ID: "x", Harness: session.HarnessCodex, Status: session.StatusEnded}
	err := harness.WakeSession(context.Background(), s, "hi")
	if err == nil || errors.Is(err, waker.ErrNoWaker) {
		t.Fatalf("got %v, want a not-active error", err)
	}
}

func TestCodexHasWaker(t *testing.T) {
	spec, ok := harness.For(session.HarnessCodex)
	if !ok || spec.Wake == nil {
		t.Fatal("codex spec has no waker")
	}
}
