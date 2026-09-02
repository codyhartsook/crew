package wake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/harness/waker"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

type fakeEnder struct {
	keys   []string
	reason string
	err    error
}

func (f *fakeEnder) End(_ context.Context, key string, _ time.Time, reason string) error {
	f.keys = append(f.keys, key)
	f.reason = reason
	return f.err
}

func codexSession() *session.Session {
	return &session.Session{ID: "t1", Harness: session.HarnessCodex, Status: session.StatusActive}
}

func wakeWith(err error) wakeFunc {
	return func(context.Context, *session.Session, string) error { return err }
}

func TestDeliverRetiresGoneSession(t *testing.T) {
	e := &fakeEnder{}
	gone := errors.Join(waker.ErrSessionGone, errors.New("no rollout found for thread id t1"))
	if err := deliver(context.Background(), e, wakeWith(gone), codexSession(), "hi"); !errors.Is(err, waker.ErrSessionGone) {
		t.Fatalf("got %v, want ErrSessionGone", err)
	}
	if len(e.keys) != 1 || e.keys[0] != "codex:t1" {
		t.Fatalf("ended %v, want [codex:t1]", e.keys)
	}
	if e.reason == "" {
		t.Error("retired without a reason")
	}
}

func TestDeliverLeavesLiveSessionAlone(t *testing.T) {
	for name, err := range map[string]error{
		"success":   nil,
		"no waker":  waker.ErrNoWaker,
		"transient": errors.New("wake codex t1: outbound queue is full"),
	} {
		t.Run(name, func(t *testing.T) {
			e := &fakeEnder{}
			if got := deliver(context.Background(), e, wakeWith(err), codexSession(), "hi"); !errors.Is(got, err) {
				t.Fatalf("got %v, want %v", got, err)
			}
			if len(e.keys) != 0 {
				t.Fatalf("retired %v, want none", e.keys)
			}
		})
	}
}

func TestDeliverReportsFailedRetirement(t *testing.T) {
	e := &fakeEnder{err: errors.New("db down")}
	err := deliver(context.Background(), e, wakeWith(waker.ErrSessionGone), codexSession(), "hi")
	if !errors.Is(err, waker.ErrSessionGone) {
		t.Errorf("lost ErrSessionGone: %v", err)
	}
	if err == nil || !errors.Is(err, e.err) {
		t.Errorf("lost the store error: %v", err)
	}
}

func TestDeliverToleratesUnknownSession(t *testing.T) {
	e := &fakeEnder{err: store.ErrNotFound}
	err := deliver(context.Background(), e, wakeWith(waker.ErrSessionGone), codexSession(), "hi")
	if !errors.Is(err, waker.ErrSessionGone) || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want bare ErrSessionGone", err)
	}
}
