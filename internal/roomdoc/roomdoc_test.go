package roomdoc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codyhartsook/multiplayer/internal/rolemem"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/roomdoc"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
)

// The generated snapshot is world readable, so a role's private memory must
// never reach it: memory is a separate table, not a visibility flag on
// entries, and this is the one place a missed filter would be expensive.
func TestSnapshotExcludesRoleMemory(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	r := room.Room{Key: "/src/widget", Scope: room.ScopeRepo, Name: "widget"}
	sess := &session.Session{
		ID: "a", Harness: session.HarnessCodex, Status: session.StatusActive,
		StartedAt: time.Now(), LastSeen: time.Now(),
	}
	if err := st.Upsert(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := st.Join(ctx, &room.Membership{SessionKey: sess.Key(), Room: r.Key, Scope: r.Scope, JoinedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.Post(ctx, &room.Entry{
		Room: r.Key, Scope: r.Scope, Mode: room.ModeNote, Author: sess.Key(),
		Body: "a public note", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteMemory(ctx, &rolemem.Entry{
		Room: r.Key, Role: "tester", Author: sess.Key(),
		Body: "a private finding", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	roomDir := t.TempDir()
	documentDir := filepath.Join(roomDir, "documents")
	if err := os.MkdirAll(documentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := roomdoc.Write(ctx, st, st, documentDir, r); err != nil {
		t.Fatalf("Write: %v", err)
	}

	snapshot, err := os.ReadFile(filepath.Join(roomDir, "ROOM.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(snapshot), "a public note") {
		t.Errorf("snapshot = %q, want the public note", snapshot)
	}
	if strings.Contains(string(snapshot), "a private finding") {
		t.Errorf("snapshot leaked role memory: %s", snapshot)
	}
}
