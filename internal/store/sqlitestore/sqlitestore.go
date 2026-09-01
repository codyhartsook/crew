// Package sqlitestore implements store.Store on top of SQLite.
//
// Location lives in flat, indexed columns rather than a JSON blob, so "which
// agents are in this worktree" stays a plain indexed query. The driver is
// modernc.org/sqlite, so the binary builds without cgo.
package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// timeFormat is the on-disk encoding for timestamps. RFC3339 with nanoseconds
// sorts lexically in the same order it sorts chronologically, which lets SQLite
// order by it directly.
const timeFormat = time.RFC3339Nano

// SchemaVersion is the store layout this binary understands. Raise it whenever
// a migration changes what older binaries can safely assume.
const SchemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
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
);

CREATE INDEX IF NOT EXISTS sessions_status    ON sessions(status);
CREATE INDEX IF NOT EXISTS sessions_harness   ON sessions(harness);
CREATE INDEX IF NOT EXISTS sessions_repo_root ON sessions(repo_root);
CREATE INDEX IF NOT EXISTS sessions_repo_name ON sessions(repo_name);
CREATE INDEX IF NOT EXISTS sessions_last_seen ON sessions(last_seen DESC);
`

// columns is the projection every read shares, so scanRow stays in step with it.
const columns = `key, id, harness, status, pid, host, username, cwd,
    has_repo, repo_name, repo_root, repo_main_root, repo_remote, repo_branch, repo_head, repo_detached, repo_is_worktree,
    has_treehouse, th_pool, th_slot, th_root, th_leased, th_lease_id, th_lease_holder,
    started_at, last_seen, ended_at, end_reason, meta`

// Store is a SQLite-backed store.Store.
type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

// Open opens (creating if needed) the database at path and applies the schema.
// The special path ":memory:" gives a private in-memory database, which the
// tests use.
func Open(path string) (*Store, error) {
	dsn, err := dsnFor(path)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	// Several harnesses fire hooks at once. WAL plus a busy timeout lets readers
	// through while one writer holds the lock, and serialising writers here
	// keeps that timeout from being spent on self-contention.
	db.SetMaxOpenConns(8)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate creates missing tables, adds columns a database created by an earlier
// version lacks, then builds the indexes. Column additions come before indexes
// because some indexed columns arrived after the table first shipped.
func migrate(db *sql.DB) error {
	ctx := context.Background()

	// Refuse a database a newer binary has already migrated. Forward migration
	// is handled below; going backwards is not, and failing loudly beats
	// writing rows an older layout cannot represent.
	var found int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&found); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if found > SchemaVersion {
		return fmt.Errorf("database is schema version %d, this multiplayer understands %d: upgrade the binary", found, SchemaVersion)
	}

	if _, err := db.ExecContext(ctx, schema+roomSchema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	for _, stmt := range columnMigrations {
		if _, err := db.ExecContext(ctx, stmt); err != nil && !isDuplicateColumn(err) {
			return fmt.Errorf("migrate: %s: %w", stmt, err)
		}
	}
	if _, err := db.ExecContext(ctx, roomIndexes); err != nil {
		return fmt.Errorf("create indexes: %w", err)
	}
	// PRAGMA takes no parameters; the value is a constant.
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion)); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	return nil
}

// isDuplicateColumn reports the error SQLite gives when a column is already
// there, which is the normal outcome on every open after the first.
func isDuplicateColumn(err error) bool {
	return strings.Contains(err.Error(), "duplicate column name")
}

// dsnFor builds the connection string, creating the parent directory for a
// file-backed database.
func dsnFor(path string) (string, error) {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		// A shared cache keeps the single in-memory database alive across the
		// pooled connections database/sql hands out.
		return "file::memory:?cache=shared&_pragma=busy_timeout(5000)", nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create store directory %s: %w", dir, err)
		}
	}
	pragmas := []string{
		"_pragma=busy_timeout(5000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
	}
	return "file:" + path + "?" + strings.Join(pragmas, "&"), nil
}

func (s *Store) Close() error { return s.db.Close() }

// started_at is written only on insert, so a resumed session keeps its
// first-seen time.
func (s *Store) Upsert(ctx context.Context, sess *session.Session) error {
	if sess == nil {
		return errors.New("upsert: nil session")
	}
	if sess.ID == "" {
		return errors.New("upsert: session has no id")
	}

	meta, err := json.Marshal(orEmptyMap(sess.Meta))
	if err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}
	repo := sess.Repo
	if repo == nil {
		repo = &session.Repo{}
	}
	th := sess.Treehouse
	if th == nil {
		th = &session.Treehouse{}
	}

	const q = `
INSERT INTO sessions (` + columns + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    status = excluded.status,
    pid = excluded.pid,
    host = excluded.host,
    username = excluded.username,
    cwd = excluded.cwd,
    has_repo = excluded.has_repo,
    repo_name = excluded.repo_name,
    repo_root = excluded.repo_root,
    repo_main_root = excluded.repo_main_root,
    repo_remote = excluded.repo_remote,
    repo_branch = excluded.repo_branch,
    repo_head = excluded.repo_head,
    repo_detached = excluded.repo_detached,
    repo_is_worktree = excluded.repo_is_worktree,
    has_treehouse = excluded.has_treehouse,
    th_pool = excluded.th_pool,
    th_slot = excluded.th_slot,
    th_root = excluded.th_root,
    th_leased = excluded.th_leased,
    th_lease_id = excluded.th_lease_id,
    th_lease_holder = excluded.th_lease_holder,
    last_seen = excluded.last_seen,
    ended_at = excluded.ended_at,
    end_reason = excluded.end_reason,
    meta = excluded.meta`

	_, err = s.db.ExecContext(ctx, q,
		sess.Key(), sess.ID, string(sess.Harness), string(sess.Status), sess.PID, sess.Host, sess.User, sess.CWD,
		sess.Repo != nil, repo.Name, repo.Root, repo.MainRoot, repo.Remote, repo.Branch, repo.Head, repo.Detached, repo.IsWorktree,
		sess.Treehouse != nil, th.Pool, th.Slot, th.Root, th.Leased, th.LeaseID, th.LeaseHolder,
		formatTime(sess.StartedAt), formatTime(sess.LastSeen), formatTimePtr(sess.EndedAt), sess.EndReason, string(meta),
	)
	if err != nil {
		return fmt.Errorf("upsert session %s: %w", sess.Key(), err)
	}
	return nil
}

// An already-ended session keeps its end time, so a duplicate SessionEnd is
// harmless.
func (s *Store) End(ctx context.Context, key string, at time.Time, reason string) error {
	const q = `
UPDATE sessions
SET status = ?, ended_at = ?, end_reason = ?, last_seen = ?
WHERE key = ? AND ended_at IS NULL`

	res, err := s.db.ExecContext(ctx, q, string(session.StatusEnded), formatTime(at), reason, formatTime(at), key)
	if err != nil {
		return fmt.Errorf("end session %s: %w", key, err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return nil
	}
	// No row changed: either the key is unknown, or the session already ended.
	if _, err := s.Get(ctx, key); err != nil {
		return err
	}
	return nil
}

func (s *Store) Touch(ctx context.Context, key string, at time.Time) error {
	const q = `UPDATE sessions SET last_seen = ? WHERE key = ? AND ended_at IS NULL`
	res, err := s.db.ExecContext(ctx, q, formatTime(at), key)
	if err != nil {
		return fmt.Errorf("touch session %s: %w", key, err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return nil
	}
	// Nothing changed: the key is unknown, or the session has already ended.
	_, err = s.Get(ctx, key)
	return err
}

func (s *Store) Get(ctx context.Context, key string) (*session.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM sessions WHERE key = ?`, key)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", key, store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", key, err)
	}
	return sess, nil
}

func (s *Store) List(ctx context.Context, f store.Filter) ([]*session.Session, error) {
	var (
		where []string
		args  []any
	)
	if f.Harness != "" {
		where = append(where, "harness = ?")
		args = append(args, string(f.Harness))
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	}
	if f.RepoName != "" {
		where = append(where, "repo_name = ?")
		args = append(args, f.RepoName)
	}
	if f.RepoRoot != "" {
		where = append(where, "repo_root = ?")
		args = append(args, f.RepoRoot)
	}
	if f.TreehouseOnly {
		where = append(where, "has_treehouse = 1")
	}

	q := `SELECT ` + columns + ` FROM sessions`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY last_seen DESC, key"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	out := []*session.Session{}
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return out, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete session %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session %s: %w", key, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", key, store.ErrNotFound)
	}
	return nil
}

// scanner covers both *sql.Row and *sql.Rows so one scan path serves Get and List.
type scanner interface {
	Scan(dest ...any) error
}

func scanSession(sc scanner) (*session.Session, error) {
	var (
		sess      session.Session
		key       string
		harness   string
		status    string
		hasRepo   bool
		repo      session.Repo
		hasTH     bool
		th        session.Treehouse
		startedAt string
		lastSeen  string
		endedAt   sql.NullString
		metaJSON  string
	)

	err := sc.Scan(
		&key, &sess.ID, &harness, &status, &sess.PID, &sess.Host, &sess.User, &sess.CWD,
		&hasRepo, &repo.Name, &repo.Root, &repo.MainRoot, &repo.Remote, &repo.Branch, &repo.Head, &repo.Detached, &repo.IsWorktree,
		&hasTH, &th.Pool, &th.Slot, &th.Root, &th.Leased, &th.LeaseID, &th.LeaseHolder,
		&startedAt, &lastSeen, &endedAt, &sess.EndReason, &metaJSON,
	)
	if err != nil {
		return nil, err
	}

	sess.Harness = session.Harness(harness)
	sess.Status = session.Status(status)
	if hasRepo {
		sess.Repo = &repo
	}
	if hasTH {
		sess.Treehouse = &th
	}
	if sess.StartedAt, err = parseTime(startedAt); err != nil {
		return nil, fmt.Errorf("parse started_at: %w", err)
	}
	if sess.LastSeen, err = parseTime(lastSeen); err != nil {
		return nil, fmt.Errorf("parse last_seen: %w", err)
	}
	if endedAt.Valid {
		t, err := parseTime(endedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse ended_at: %w", err)
		}
		sess.EndedAt = &t
	}
	if metaJSON != "" {
		if err := json.Unmarshal([]byte(metaJSON), &sess.Meta); err != nil {
			return nil, fmt.Errorf("decode meta: %w", err)
		}
	}
	if len(sess.Meta) == 0 {
		sess.Meta = nil
	}
	return &sess, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(timeFormat) }

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
