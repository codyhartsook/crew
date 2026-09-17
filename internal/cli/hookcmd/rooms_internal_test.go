package hookcmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/hook"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

func roomTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// arrival registers a session sitting in dir and posts one entry from somebody
// else, so the start path has something it could have shown.
func arrival(t *testing.T, st *sqlitestore.Store, dir string, mode room.Mode, body string) *session.Session {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	sess := &session.Session{
		ID: "arriving", Harness: session.HarnessCodex, Status: session.StatusActive,
		Place: session.Place{
			CWD:    dir,
			Folder: &session.Folder{Name: filepath.Base(dir), Root: dir},
		},
		StartedAt: now, LastSeen: now,
	}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := st.Post(ctx, &room.Entry{
		Room: dir, Scope: room.ScopeFolder, Mode: mode,
		Author: "codex:someone-else", Body: body, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return sess
}

// Arrival must not acknowledge the room: an agent that was never shown an
// entry has not read it, and silently acking would lose it for good.
func TestSessionStartLeavesEntriesUnread(t *testing.T) {
	ctx := context.Background()
	st := roomTestStore(t)
	dir := t.TempDir()
	sess := arrival(t, st, dir, room.ModeRequest, "can you rerun the flake?")

	if _, err := roomsFor(ctx, st, hook.EventStart, sess, sess.Key()); err != nil {
		t.Fatalf("roomsFor: %v", err)
	}

	unread, err := st.Unread(ctx, sess.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 {
		t.Fatalf("unread after arrival = %d, want 1: the start path must not ack", len(unread))
	}

	// The next turn is where the backlog is offered.
	notice, err := roomsFor(ctx, st, hook.EventPrompt, nil, sess.Key())
	if err != nil {
		t.Fatalf("roomsFor prompt: %v", err)
	}
	if !strings.Contains(notice, "unread in this room") {
		t.Errorf("turn notice = %q, want it to offer the unread entry", notice)
	}
}
