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
	"github.com/codyhartsook/multiplayer/internal/usage"
)

// timeFormat is the on-disk encoding for timestamps. RFC3339 with nanoseconds
// sorts lexically in the same order it sorts chronologically, which lets SQLite
// order by it directly.
const timeFormat = time.RFC3339Nano

// SchemaVersion is the store layout this binary understands. Raise it whenever
// a migration changes what older binaries can safely assume.
const SchemaVersion = 6

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    key              TEXT PRIMARY KEY,
    id               TEXT NOT NULL,
    harness          TEXT NOT NULL,
    alias            TEXT NOT NULL DEFAULT '',
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

    has_pool         INTEGER NOT NULL DEFAULT 0,
    pool_manager     TEXT NOT NULL DEFAULT '',
    pool_name        TEXT NOT NULL DEFAULT '',
    pool_slot        TEXT NOT NULL DEFAULT '',
    pool_root        TEXT NOT NULL DEFAULT '',
    pool_leased      INTEGER NOT NULL DEFAULT 0,
    pool_lease_id    TEXT NOT NULL DEFAULT '',
    pool_lease_holder TEXT NOT NULL DEFAULT '',

    has_folder       INTEGER NOT NULL DEFAULT 0,
    folder_name      TEXT NOT NULL DEFAULT '',
    folder_root      TEXT NOT NULL DEFAULT '',

    started_at       TEXT NOT NULL,
    last_seen        TEXT NOT NULL,
    ended_at         TEXT,
    end_reason       TEXT NOT NULL DEFAULT '',
    meta             TEXT NOT NULL DEFAULT '{}',
    usage            TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS sessions_status    ON sessions(status);
CREATE INDEX IF NOT EXISTS sessions_harness   ON sessions(harness);
CREATE INDEX IF NOT EXISTS sessions_repo_root ON sessions(repo_root);
CREATE INDEX IF NOT EXISTS sessions_repo_name ON sessions(repo_name);
CREATE INDEX IF NOT EXISTS sessions_last_seen ON sessions(last_seen DESC);
`

// columns is the projection every read shares, so scanRow stays in step with it.
const columns = `key, id, harness, alias, status, pid, host, username, cwd,
    has_repo, repo_name, repo_root, repo_main_root, repo_remote, repo_branch, repo_head, repo_detached, repo_is_worktree,
    has_pool, pool_manager, pool_name, pool_slot, pool_root, pool_leased, pool_lease_id, pool_lease_holder,
    has_folder, folder_name, folder_root,
    started_at, last_seen, ended_at, end_reason, meta, usage`

const aliasIndex = `CREATE UNIQUE INDEX IF NOT EXISTS sessions_active_alias
ON sessions(alias) WHERE status = 'active' AND alias != ''`

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

// additive lists the statements that carry a database up to each version.
// SQLite adds a column in place, so a version reachable this way keeps every
// row; a version that needs more than this still demands a fresh database.
var additive = map[int][]string{
	6: {
		`ALTER TABLE sessions ADD COLUMN has_folder INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN folder_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN folder_root TEXT NOT NULL DEFAULT ''`,
	},
}

// upgrade walks a database forward one version at a time and reports the
// version it reached, stopping short at the first one with no additive path.
// All of it commits or none does, so a failure never leaves a half-migrated
// layout behind a stale user_version.
func upgrade(ctx context.Context, db *sql.DB, from int) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return from, fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()

	reached := from
	for v := from + 1; v <= SchemaVersion; v++ {
		steps, ok := additive[v]
		if !ok {
			break
		}
		for _, step := range steps {
			if _, err := tx.ExecContext(ctx, step); err != nil {
				return from, fmt.Errorf("migrate to schema version %d: %w", v, err)
			}
		}
		reached = v
	}
	if reached == from {
		return from, nil
	}
	if err := tx.Commit(); err != nil {
		return from, fmt.Errorf("commit migration: %w", err)
	}
	return reached, nil
}

// migrate creates a fresh schema or verifies that an existing one matches it.
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
		return fmt.Errorf("database is schema version %d, this crew understands %d: upgrade the binary", found, SchemaVersion)
	}
	if found > 0 && found < SchemaVersion {
		reached, err := upgrade(ctx, db, found)
		if err != nil {
			return err
		}
		if reached < SchemaVersion {
			return fmt.Errorf("database is schema version %d, crew now requires a fresh version %d database: move the old database aside and restart", found, SchemaVersion)
		}
	}
	var legacy int
	if found == 0 && db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name IN ('sessions', 'entries') LIMIT 1`).Scan(&legacy) == nil {
		return fmt.Errorf("crew now requires a fresh version %d database: move the unversioned database aside and restart", SchemaVersion)
	}

	if _, err := db.ExecContext(ctx, schema+roomSchema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, aliasIndex); err != nil {
		return fmt.Errorf("create alias index: %w", err)
	}
	if err := backfillAliases(ctx, db); err != nil {
		return err
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
	for attempts := 0; attempts < 256; attempts++ {
		alias, err := s.aliasFor(ctx, sess.Key(), sess.Status)
		if err != nil {
			return err
		}
		sess.Alias = alias
		if err := s.upsert(ctx, sess); err == nil {
			return nil
		} else if !isAliasConflict(err) {
			return err
		}
	}
	return errors.New("assign session alias: no names available")
}

func (s *Store) upsert(ctx context.Context, sess *session.Session) error {
	meta, err := json.Marshal(orEmptyMap(sess.Meta))
	if err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}
	// Empty rather than "null" when absent, so a session that never reported
	// usage reads back as nil instead of a zero snapshot.
	usageJSON := ""
	if sess.Usage != nil {
		encoded, err := json.Marshal(sess.Usage)
		if err != nil {
			return fmt.Errorf("encode usage: %w", err)
		}
		usageJSON = string(encoded)
	}
	repo := sess.Repo
	if repo == nil {
		repo = &session.Repo{}
	}
	pool := sess.Pool
	if pool == nil {
		pool = &session.Pool{}
	}
	folder := sess.Folder
	if folder == nil {
		folder = &session.Folder{}
	}

	const q = `
INSERT INTO sessions (` + columns + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?, ?, ?, ?, ?, ?,
        ?, ?, ?,
        ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    status = excluded.status,
    alias = excluded.alias,
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
    has_pool = excluded.has_pool,
    pool_manager = excluded.pool_manager,
    pool_name = excluded.pool_name,
    pool_slot = excluded.pool_slot,
    pool_root = excluded.pool_root,
    pool_leased = excluded.pool_leased,
    pool_lease_id = excluded.pool_lease_id,
    pool_lease_holder = excluded.pool_lease_holder,
    has_folder = excluded.has_folder,
    folder_name = excluded.folder_name,
    folder_root = excluded.folder_root,
    last_seen = excluded.last_seen,
    ended_at = excluded.ended_at,
    end_reason = excluded.end_reason,
    meta = excluded.meta,
    usage = excluded.usage`

	_, err = s.db.ExecContext(ctx, q,
		sess.Key(), sess.ID, string(sess.Harness), sess.Alias, string(sess.Status), sess.PID, sess.Host, sess.User, sess.CWD,
		sess.Repo != nil, repo.Name, repo.Root, repo.MainRoot, repo.Remote, repo.Branch, repo.Head, repo.Detached, repo.IsWorktree,
		sess.Pool != nil, pool.Manager, pool.Name, pool.Slot, pool.Root, pool.Leased, pool.LeaseID, pool.LeaseHolder,
		sess.Folder != nil, folder.Name, folder.Root,
		formatTime(sess.StartedAt), formatTime(sess.LastSeen), formatTimePtr(sess.EndedAt), sess.EndReason, string(meta), usageJSON,
	)
	if err != nil {
		return fmt.Errorf("upsert session %s: %w", sess.Key(), err)
	}
	return nil
}

func (s *Store) aliasFor(ctx context.Context, key string, status session.Status) (string, error) {
	if status != session.StatusActive {
		return "", nil
	}
	var alias string
	err := s.db.QueryRowContext(ctx, `SELECT alias FROM sessions WHERE key = ? AND status = ?`, key, session.StatusActive).Scan(&alias)
	switch {
	case err == nil && alias != "":
		return alias, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("read session alias: %w", err)
	}
	used, err := activeAliases(ctx, s.db)
	if err != nil {
		return "", err
	}
	alias, ok := session.RandomAlias(used)
	if !ok {
		return "", errors.New("assign session alias: no names available")
	}
	return alias, nil
}

func activeAliases(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT alias FROM sessions WHERE status = ? AND alias != ''`, session.StatusActive)
	if err != nil {
		return nil, fmt.Errorf("list session aliases: %w", err)
	}
	defer rows.Close()
	used := map[string]bool{}
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, fmt.Errorf("read session alias: %w", err)
		}
		used[alias] = true
	}
	return used, rows.Err()
}

func backfillAliases(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT key FROM sessions WHERE status = ? AND alias = '' ORDER BY key`, session.StatusActive)
	if err != nil {
		return fmt.Errorf("list sessions missing aliases: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return fmt.Errorf("read session missing alias: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list sessions missing aliases: %w", err)
	}
	for _, key := range keys {
		for attempts := 0; attempts < 256; attempts++ {
			used, err := activeAliases(ctx, db)
			if err != nil {
				return err
			}
			alias, ok := session.RandomAlias(used)
			if !ok {
				return errors.New("assign session alias: no names available")
			}
			res, err := db.ExecContext(ctx, `UPDATE sessions SET alias = ? WHERE key = ? AND status = ? AND alias = ''`, alias, key, session.StatusActive)
			if err == nil {
				if n, _ := res.RowsAffected(); n != 0 {
					break
				}
				break
			}
			if !isAliasConflict(err) {
				return fmt.Errorf("assign alias to %s: %w", key, err)
			}
		}
	}
	return nil
}

func isAliasConflict(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed: sessions.alias")
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

func (s *Store) SetUsage(ctx context.Context, key string, u *usage.Snapshot) error {
	if u == nil {
		return nil
	}
	encoded, err := json.Marshal(u)
	if err != nil {
		return fmt.Errorf("encode usage: %w", err)
	}
	const q = `UPDATE sessions SET usage = ? WHERE key = ? AND ended_at IS NULL`
	res, err := s.db.ExecContext(ctx, q, string(encoded), key)
	if err != nil {
		return fmt.Errorf("set usage %s: %w", key, err)
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
	if f.PooledOnly {
		where = append(where, "has_pool = 1")
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
		hasPool   bool
		pool      session.Pool
		hasFolder bool
		folder    session.Folder
		startedAt string
		lastSeen  string
		endedAt   sql.NullString
		metaJSON  string
		usageJSON string
	)

	err := sc.Scan(
		&key, &sess.ID, &harness, &sess.Alias, &status, &sess.PID, &sess.Host, &sess.User, &sess.CWD,
		&hasRepo, &repo.Name, &repo.Root, &repo.MainRoot, &repo.Remote, &repo.Branch, &repo.Head, &repo.Detached, &repo.IsWorktree,
		&hasPool, &pool.Manager, &pool.Name, &pool.Slot, &pool.Root, &pool.Leased, &pool.LeaseID, &pool.LeaseHolder,
		&hasFolder, &folder.Name, &folder.Root,
		&startedAt, &lastSeen, &endedAt, &sess.EndReason, &metaJSON, &usageJSON,
	)
	if err != nil {
		return nil, err
	}

	sess.Harness = session.Harness(harness)
	sess.Status = session.Status(status)
	if hasRepo {
		sess.Repo = &repo
	}
	if hasPool {
		sess.Pool = &pool
	}
	if hasFolder {
		sess.Folder = &folder
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
	if usageJSON != "" {
		var u usage.Snapshot
		if err := json.Unmarshal([]byte(usageJSON), &u); err != nil {
			return nil, fmt.Errorf("decode usage: %w", err)
		}
		sess.Usage = &u
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
