package sqlitestore_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/codyhartsook/multiplayer/internal/room"
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

// A database written by an earlier version must keep working.
func TestOpenMigratesAnOlderDatabase(t *testing.T) {
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

	s, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatalf("Open on an older database: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// The pre-existing row survives.
	existing, err := s.Entries(ctx, room.Filter{})
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(existing) != 1 || existing[0].Body != "chose sqlite" {
		t.Fatalf("entries = %+v, want the row written by the older version", existing)
	}

}

// Opening twice must not fail on the second pass over the migrations.
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

// v1Sessions is the sessions table as it shipped, with the pooled-worktree
// columns named after treehouse.
const v1Sessions = `
CREATE TABLE sessions (
    key              TEXT PRIMARY KEY,
    id               TEXT NOT NULL,
    harness          TEXT NOT NULL,
    status           TEXT NOT NULL,
    pid              INTEGER NOT NULL DEFAULT 0,
    host             TEXT NOT NULL DEFAULT '',
    username         TEXT NOT NULL DEFAULT '',
    cwd              TEXT NOT NULL DEFAULT '',
    has_repo         INTEGER NOT NULL DEFAULT 0,
    repo_name        TEXT NOT NULL DEFAULT '',
    repo_root        TEXT NOT NULL DEFAULT '',
    repo_main_root   TEXT NOT NULL DEFAULT '',
    repo_remote      TEXT NOT NULL DEFAULT '',
    repo_branch      TEXT NOT NULL DEFAULT '',
    repo_head        TEXT NOT NULL DEFAULT '',
    repo_detached    INTEGER NOT NULL DEFAULT 0,
    repo_is_worktree INTEGER NOT NULL DEFAULT 0,
    has_treehouse    INTEGER NOT NULL DEFAULT 0,
    th_pool          TEXT NOT NULL DEFAULT '',
    th_slot          TEXT NOT NULL DEFAULT '',
    th_root          TEXT NOT NULL DEFAULT '',
    th_leased        INTEGER NOT NULL DEFAULT 0,
    th_lease_id      TEXT NOT NULL DEFAULT '',
    th_lease_holder  TEXT NOT NULL DEFAULT '',
    started_at       TEXT NOT NULL,
    last_seen        TEXT NOT NULL,
    ended_at         TEXT,
    end_reason       TEXT NOT NULL DEFAULT '',
    meta             TEXT NOT NULL DEFAULT '{}'
);`

// A database written before pools were generalized must keep its slot and lease
// after migration, under whichever manager it came from.
func TestMigrateCarriesTreehouseColumnsForward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(v1Sessions); err != nil {
		t.Fatalf("create v1 schema: %v", err)
	}
	_, err = db.Exec(`INSERT INTO sessions
        (key, id, harness, status, cwd, has_repo, repo_name, repo_root,
         has_treehouse, th_pool, th_slot, th_root, th_leased, th_lease_id, th_lease_holder,
         started_at, last_seen)
        VALUES ('codex:old', 'old', 'codex', 'active', '/pool/widget-abc/3/widget',
                1, 'widget', '/pool/widget-abc/3/widget',
                1, 'widget-abc', '3', '/pool/widget-abc/3/widget', 1, '70b6d0fd', 'agent:x',
                '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert v1 row: %v", err)
	}
	db.Close()

	s, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	got, err := s.Get(context.Background(), "codex:old")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Pool == nil {
		t.Fatal("Pool = nil, want the migrated slot")
	}
	want := session.Pool{
		Manager: "treehouse", Name: "widget-abc", Slot: "3",
		Root: "/pool/widget-abc/3/widget", Leased: true,
		LeaseID: "70b6d0fd", LeaseHolder: "agent:x",
	}
	if *got.Pool != want {
		t.Errorf("Pool = %+v, want %+v", *got.Pool, want)
	}

	// The filter must see it too, since it reads the new column.
	pooled, err := s.List(context.Background(), store.Filter{PooledOnly: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pooled) != 1 {
		t.Errorf("pooled sessions = %d, want 1", len(pooled))
	}
}
