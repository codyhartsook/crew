package sqlitestore_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/codyhartsook/multiplayer/internal/session"

	"github.com/codyhartsook/multiplayer/internal/store"
	"github.com/codyhartsook/multiplayer/internal/store/sqlitestore"
	"github.com/codyhartsook/multiplayer/internal/store/storetest"
)

func TestStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		// A file in a temp dir rather than :memory:, so the suite exercises the
		// WAL and busy-timeout settings the hook actually runs against.
		s, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}

func TestRoomStore(t *testing.T) {
	storetest.RunRooms(t, func(t *testing.T) store.RoomStore {
		s, err := sqlitestore.Open(filepath.Join(t.TempDir(), "sessions.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}

func TestOpenRefusesAnOlderDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	// The entries table as it was before reviews existed.
	const old = `
CREATE TABLE entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    room       TEXT    NOT NULL,
    scope      TEXT    NOT NULL,
    kind       TEXT    NOT NULL,
    author     TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    resolves   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);
INSERT INTO entries (room, scope, kind, author, body, created_at)
VALUES ('/src/widget', 'worktree', 'decision', 'codex:a', 'chose sqlite', '2026-09-01T12:00:00Z');`
	if _, err := db.Exec(old); err != nil {
		t.Fatalf("seed old schema: %v", err)
	}
	db.Close()

	_, err = sqlitestore.Open(path)
	if err == nil || !strings.Contains(err.Error(), "fresh") {
		t.Fatalf("Open on an older database = %v, want fresh-database guidance", err)
	}
}

// Opening the current schema repeatedly is safe.
func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	for i := 0; i < 3; i++ {
		s, err := sqlitestore.Open(path)
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		s.Close()
	}
}

// Going forward is migration; going backwards is not handled, so a database a
// newer binary has migrated must be refused rather than half-understood.
func TestOpenRefusesANewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.db")
	s, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", sqlitestore.SchemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = sqlitestore.Open(path)
	if err == nil {
		t.Fatal("Open accepted a database from a newer binary")
	}
	if !strings.Contains(err.Error(), "upgrade the binary") {
		t.Errorf("error = %v, want it to say what to do", err)
	}
}

func TestOpenRecordsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.db")
	s, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.Close()

	db, _ := sql.Open("sqlite", "file:"+path)
	defer db.Close()
	var got int
	if err := db.QueryRow("PRAGMA user_version").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != sqlitestore.SchemaVersion {
		t.Errorf("user_version = %d, want %d", got, sqlitestore.SchemaVersion)
	}
}

func TestEndedAliasCanBeReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reclaim.db")
	s, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := "2026-09-02T12:00:00Z"
	first := &session.Session{
		ID: "first", Harness: session.HarnessCodex, Status: session.StatusActive,
		StartedAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
	}
	if err := s.Upsert(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := s.End(context.Background(), first.Key(), first.LastSeen, "exit"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO sessions
        (key, id, harness, alias, status, started_at, last_seen)
        VALUES ('claude:second', 'second', 'claude', ?, 'active', ?, ?)`, first.Alias, now, now)
	if err != nil {
		t.Fatalf("reuse ended alias %q: %v", first.Alias, err)
	}
}
