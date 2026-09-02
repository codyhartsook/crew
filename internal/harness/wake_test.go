package harness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/codyhartsook/multiplayer/internal/harness"
	"github.com/codyhartsook/multiplayer/internal/session"
)

func TestWakeSessionWithoutWaker(t *testing.T) {
	for _, h := range []session.Harness{session.HarnessClaude, session.HarnessUnknown, session.Harness("nope")} {
		s := &session.Session{ID: "x", Harness: h, Status: session.StatusActive}
		if err := harness.WakeSession(context.Background(), s, "hi"); !errors.Is(err, harness.ErrNoWaker) {
			t.Errorf("%s: got %v, want ErrNoWaker", h, err)
		}
	}
}

func TestWakeSessionRejectsEnded(t *testing.T) {
	s := &session.Session{ID: "x", Harness: session.HarnessCodex, Status: session.StatusEnded}
	err := harness.WakeSession(context.Background(), s, "hi")
	if err == nil || errors.Is(err, harness.ErrNoWaker) {
		t.Fatalf("got %v, want a not-active error", err)
	}
}

func TestCodexHasWaker(t *testing.T) {
	spec, ok := harness.For(session.HarnessCodex)
	if !ok || spec.Wake == nil {
		t.Fatal("codex spec has no waker")
	}
	if spec.Delivery != harness.DeliverOnIdle {
		t.Errorf("delivery = %q, want %q", spec.Delivery, harness.DeliverOnIdle)
	}
}
