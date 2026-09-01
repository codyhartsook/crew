package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// roomSchema is applied alongside the session schema.
//
// AUTOINCREMENT matters here: it guarantees entry ids are never reused, so a
// per-session cursor can be a single integer watermark across every room.
const roomSchema = `
CREATE TABLE IF NOT EXISTS entries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    room       TEXT    NOT NULL,
    scope      TEXT    NOT NULL,
    kind       TEXT    NOT NULL,
    author     TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    resolves   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS memberships (
    session_key TEXT NOT NULL,
    room        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    joined_at   TEXT NOT NULL,
    PRIMARY KEY (session_key, room)
);

CREATE TABLE IF NOT EXISTS state (
    room       TEXT    NOT NULL,
    key        TEXT    NOT NULL,
    value      TEXT    NOT NULL,
    scope      TEXT    NOT NULL,
    author     TEXT    NOT NULL,
    revision   INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (room, key)
);

CREATE TABLE IF NOT EXISTS cursors (
    session_key TEXT PRIMARY KEY,
    last_seen   INTEGER NOT NULL
);
`

const roomIndexes = `
CREATE INDEX IF NOT EXISTS entries_room     ON entries(room);
CREATE INDEX IF NOT EXISTS entries_resolves ON entries(resolves) WHERE resolves != 0;
CREATE INDEX IF NOT EXISTS memberships_room ON memberships(room);
CREATE INDEX IF NOT EXISTS state_room       ON state(room);
`

var _ store.RoomStore = (*Store)(nil)

// entryColumns is the projection every entry read shares. resolved_by is
// derived rather than stored, so resolution stays an append and never a write
// back over somebody else's row.
const entryColumns = `e.id, e.room, e.scope, e.kind, e.author, e.body, e.resolves,
    COALESCE(r.id, 0) AS resolved_by, e.created_at`

const entryFrom = ` FROM entries e LEFT JOIN entries r ON r.resolves = e.id`

func (s *Store) Join(ctx context.Context, m *room.Membership) error {
	if m == nil || m.SessionKey == "" || m.Room == "" {
		return errors.New("join: session key and room are required")
	}
	const q = `
INSERT INTO memberships (session_key, room, scope, joined_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(session_key, room) DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, m.SessionKey, m.Room, string(m.Scope), formatTime(m.JoinedAt)); err != nil {
		return fmt.Errorf("join %s to %s: %w", m.SessionKey, m.Room, err)
	}
	return nil
}

func (s *Store) Leave(ctx context.Context, sessionKey, roomKey string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM memberships WHERE session_key = ? AND room = ?`, sessionKey, roomKey)
	if err != nil {
		return fmt.Errorf("leave %s: %w", roomKey, err)
	}
	return nil
}

func (s *Store) Rooms(ctx context.Context, sessionKey string) ([]*room.Membership, error) {
	const q = `SELECT session_key, room, scope, joined_at FROM memberships WHERE session_key = ? ORDER BY joined_at, room`
	return s.memberships(ctx, q, sessionKey)
}

func (s *Store) Members(ctx context.Context, roomKey string) ([]*room.Membership, error) {
	const q = `SELECT session_key, room, scope, joined_at FROM memberships WHERE room = ? ORDER BY joined_at, session_key`
	return s.memberships(ctx, q, roomKey)
}

func (s *Store) memberships(ctx context.Context, query string, arg string) ([]*room.Membership, error) {
	rows, err := s.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	defer rows.Close()

	out := []*room.Membership{}
	for rows.Next() {
		var (
			m        room.Membership
			scope    string
			joinedAt string
		)
		if err := rows.Scan(&m.SessionKey, &m.Room, &scope, &joinedAt); err != nil {
			return nil, fmt.Errorf("list memberships: %w", err)
		}
		m.Scope = room.Scope(scope)
		if m.JoinedAt, err = parseTime(joinedAt); err != nil {
			return nil, fmt.Errorf("parse joined_at: %w", err)
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *Store) Post(ctx context.Context, e *room.Entry) error {
	if e == nil {
		return errors.New("post: nil entry")
	}
	if e.Room == "" || e.Author == "" {
		return errors.New("post: room and author are required")
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("post: unknown kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Body) == "" {
		return errors.New("post: body is empty")
	}

	const q = `
INSERT INTO entries (room, scope, kind, author, body, resolves,
                     created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, q,
		e.Room, string(e.Scope), string(e.Kind), e.Author, e.Body, e.Resolves, formatTime(e.CreatedAt))
	if err != nil {
		return fmt.Errorf("post entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("post entry: %w", err)
	}
	e.ID = id
	return nil
}

func (s *Store) Entries(ctx context.Context, f room.Filter) ([]*room.Entry, error) {
	var (
		where []string
		args  []any
	)
	if len(f.IDs) > 0 {
		where = append(where, "e.id IN ("+placeholders(len(f.IDs))+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if len(f.Rooms) > 0 {
		where = append(where, "e.room IN ("+placeholders(len(f.Rooms))+")")
		for _, r := range f.Rooms {
			args = append(args, r)
		}
	}
	if len(f.Kinds) > 0 {
		where = append(where, "e.kind IN ("+placeholders(len(f.Kinds))+")")
		for _, k := range f.Kinds {
			args = append(args, string(k))
		}
	}
	if f.OpenOnly {
		where = append(where, openPredicate)
	}

	q := `SELECT ` + entryColumns + entryFrom
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY e.id"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}
	return s.queryEntries(ctx, q, args...)
}

// openPredicate matches entries that still want an answer: an addressed kind,
// not itself a resolution, and nothing has closed it.
const openPredicate = `e.kind IN ('question', 'handoff', 'review') AND e.resolves = 0 AND r.id IS NULL`

// answersToMe matches a resolution of something this session asked. Without it
// an agent is never told that its own question was answered, since a resolution
// is by definition not open.
const answersToMe = `e.resolves != 0 AND EXISTS (
    SELECT 1 FROM entries t WHERE t.id = e.resolves AND t.author = ?)`

func (s *Store) Unread(ctx context.Context, sessionKey string) ([]*room.Entry, error) {
	const q = `
SELECT ` + entryColumns + entryFrom + `
WHERE e.room IN (SELECT room FROM memberships WHERE session_key = ?)
  AND e.author != ?
  AND e.id > COALESCE((SELECT last_seen FROM cursors WHERE session_key = ?), 0)
  AND ((` + openPredicate + `) OR (` + answersToMe + `))
ORDER BY e.id`
	return s.queryEntries(ctx, q, sessionKey, sessionKey, sessionKey, sessionKey)
}

func (s *Store) Ack(ctx context.Context, sessionKey string, throughID int64) error {
	const q = `
INSERT INTO cursors (session_key, last_seen) VALUES (?, ?)
ON CONFLICT(session_key) DO UPDATE SET last_seen = MAX(last_seen, excluded.last_seen)`

	if _, err := s.db.ExecContext(ctx, q, sessionKey, throughID); err != nil {
		return fmt.Errorf("ack %s: %w", sessionKey, err)
	}
	return nil
}

func (s *Store) queryEntries(ctx context.Context, query string, args ...any) ([]*room.Entry, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	out := []*room.Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("list entries: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanEntry(sc scanner) (*room.Entry, error) {
	var (
		e         room.Entry
		scope     string
		kind      string
		createdAt string
	)
	err := sc.Scan(&e.ID, &e.Room, &scope, &kind, &e.Author, &e.Body, &e.Resolves,
		&e.ResolvedBy, &createdAt)
	if err != nil {
		return nil, err
	}
	e.Scope = room.Scope(scope)
	e.Kind = room.Kind(kind)
	if e.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	return &e, nil
}

const stateColumns = `room, scope, key, value, author, revision, updated_at`

func (s *Store) SetState(ctx context.Context, st *room.State) error {
	if st == nil || st.Room == "" || st.Author == "" {
		return errors.New("set state: room and author are required")
	}
	if err := room.ValidKey(st.Key); err != nil {
		return fmt.Errorf("set state: %w", err)
	}

	// revision counts writes, so a reader can see a value that keeps changing
	// without the store keeping every version of it.
	const q = `
INSERT INTO state (` + stateColumns + `) VALUES (?, ?, ?, ?, ?, 1, ?)
ON CONFLICT(room, key) DO UPDATE SET
    value = excluded.value,
    scope = excluded.scope,
    author = excluded.author,
    revision = state.revision + 1,
    updated_at = excluded.updated_at`

	if _, err := s.db.ExecContext(ctx, q,
		st.Room, string(st.Scope), st.Key, st.Value, st.Author, formatTime(st.UpdatedAt)); err != nil {
		return fmt.Errorf("set state %s: %w", st.Key, err)
	}
	// Report the revision actually stored.
	stored, err := s.GetState(ctx, st.Room, st.Key)
	if err != nil {
		return err
	}
	st.Revision = stored.Revision
	return nil
}

func (s *Store) GetState(ctx context.Context, roomKey, key string) (*room.State, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+stateColumns+` FROM state WHERE room = ? AND key = ?`, roomKey, key)
	st, err := scanState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", key, store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get state %s: %w", key, err)
	}
	return st, nil
}

func (s *Store) States(ctx context.Context, f room.StateFilter) ([]*room.State, error) {
	var (
		where []string
		args  []any
	)
	if len(f.Rooms) > 0 {
		where = append(where, "room IN ("+placeholders(len(f.Rooms))+")")
		for _, r := range f.Rooms {
			args = append(args, r)
		}
	}
	if f.Prefix != "" {
		where = append(where, `key LIKE ? ESCAPE '!'`)
		args = append(args, escapeLike(f.Prefix)+"%")
	}

	q := `SELECT ` + stateColumns + ` FROM state`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY room, key"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list state: %w", err)
	}
	defer rows.Close()

	out := []*room.State{}
	for rows.Next() {
		st, err := scanState(rows)
		if err != nil {
			return nil, fmt.Errorf("list state: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Promote moves a thread: the entry and anything that resolves it, so a
// question and its answer never end up in different rooms.
func (s *Store) Promote(ctx context.Context, id int64, toRoom string, scope room.Scope) (int, error) {
	const q = `UPDATE entries SET room = ?, scope = ? WHERE id = ? OR resolves = ?`
	res, err := s.db.ExecContext(ctx, q, toRoom, string(scope), id, id)
	if err != nil {
		return 0, fmt.Errorf("promote entry %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("promote entry %d: %w", id, err)
	}
	if n == 0 {
		return 0, fmt.Errorf("no entry [%d]: %w", id, store.ErrNotFound)
	}
	return int(n), nil
}

func (s *Store) PromoteState(ctx context.Context, fromRoom, key, toRoom string, scope room.Scope) error {
	if _, err := s.GetState(ctx, fromRoom, key); err != nil {
		return err
	}
	// State is keyed per room, so a clash is a real conflict rather than
	// something to silently overwrite.
	if existing, err := s.GetState(ctx, toRoom, key); err == nil {
		return fmt.Errorf("%q already exists in the destination room (%s)", key, truncateValue(existing.Value))
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	const q = `UPDATE state SET room = ?, scope = ? WHERE room = ? AND key = ?`
	if _, err := s.db.ExecContext(ctx, q, toRoom, string(scope), fromRoom, key); err != nil {
		return fmt.Errorf("promote state %s: %w", key, err)
	}
	return nil
}

func truncateValue(v string) string {
	v = strings.Join(strings.Fields(v), " ")
	if len(v) > 40 {
		return v[:40] + "…"
	}
	return v
}

// Search matches with LIKE rather than a full-text index. A room holds
// hundreds of rows, not millions, so a scan is instant - and an FTS table would
// need keeping in step with the rows, which is the drift a single source of
// truth exists to avoid.
func (s *Store) Search(ctx context.Context, q room.Query) (*room.Results, error) {
	term := strings.TrimSpace(q.Text)
	if term == "" {
		return nil, errors.New("search: empty query")
	}
	like := "%" + escapeLike(term) + "%"

	results := &room.Results{State: []*room.State{}, Entries: []*room.Entry{}}
	roomFilter, roomArgs := "", []any{}
	if len(q.Rooms) > 0 {
		roomFilter = " AND room IN (" + placeholders(len(q.Rooms)) + ")"
		for _, r := range q.Rooms {
			roomArgs = append(roomArgs, r)
		}
	}

	// A key match outranks a value match: naming the thing you asked for is a
	// stronger signal than mentioning it.
	stateQ := `SELECT ` + stateColumns + ` FROM state
 WHERE (key LIKE ? ESCAPE '!' OR value LIKE ? ESCAPE '!')` + roomFilter + `
 ORDER BY CASE WHEN key LIKE ? ESCAPE '!' THEN 0 ELSE 1 END, key`
	args := append([]any{like, like}, roomArgs...)
	args = append(args, like)
	if q.Limit > 0 {
		stateQ += " LIMIT ?"
		args = append(args, q.Limit)
	}
	rows, err := s.db.QueryContext(ctx, stateQ, args...)
	if err != nil {
		return nil, fmt.Errorf("search state: %w", err)
	}
	for rows.Next() {
		st, err := scanState(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("search state: %w", err)
		}
		results.State = append(results.State, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search state: %w", err)
	}

	entryRoomFilter := strings.ReplaceAll(roomFilter, "room IN", "e.room IN")
	entryQ := `SELECT ` + entryColumns + entryFrom + `
 WHERE e.body LIKE ? ESCAPE '!'` + entryRoomFilter + `
 ORDER BY e.id DESC`
	args = append([]any{like}, roomArgs...)
	if q.Limit > 0 {
		entryQ += " LIMIT ?"
		args = append(args, q.Limit)
	}
	entries, err := s.queryEntries(ctx, entryQ, args...)
	if err != nil {
		return nil, err
	}
	results.Entries = entries
	return results, nil
}

func (s *Store) DeleteState(ctx context.Context, roomKey, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM state WHERE room = ? AND key = ?`, roomKey, key)
	if err != nil {
		return fmt.Errorf("delete state %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete state %s: %w", key, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", key, store.ErrNotFound)
	}
	return nil
}

func scanState(sc scanner) (*room.State, error) {
	var (
		st        room.State
		scope     string
		updatedAt string
	)
	if err := sc.Scan(&st.Room, &scope, &st.Key, &st.Value, &st.Author, &st.Revision, &updatedAt); err != nil {
		return nil, err
	}
	st.Scope = room.Scope(scope)
	var err error
	if st.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &st, nil
}

// escapeLike neutralises LIKE wildcards in user text. The escape character is
// "!" rather than a backslash so the SQL needs no backslash quoting.
func escapeLike(s string) string {
	for _, ch := range []string{"!", "%", "_"} {
		s = strings.ReplaceAll(s, ch, "!"+ch)
	}
	return s
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
