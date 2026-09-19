package sqlitestore

import (
	"context"
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
    mode       TEXT    NOT NULL CHECK (mode IN ('note', 'request')),
    author     TEXT    NOT NULL,
    recipient  TEXT    NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS cursors (
    session_key TEXT PRIMARY KEY,
    last_seen   INTEGER NOT NULL
);
`

const roomIndexes = `
CREATE INDEX IF NOT EXISTS entries_room     ON entries(room);
CREATE INDEX IF NOT EXISTS entries_resolves ON entries(resolves) WHERE resolves != 0;
CREATE INDEX IF NOT EXISTS memberships_room ON memberships(room);
`

var _ store.RoomStore = (*Store)(nil)

// entryColumns is the projection every entry read shares. resolved_by is
// derived rather than stored, so resolution stays an append and never a write
// back over somebody else's row.
const entryColumns = `e.id, e.room, e.scope, e.mode, e.author, e.recipient, e.body, e.resolves,
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
	if !e.Mode.Valid() {
		return fmt.Errorf("post: unknown mode %q", e.Mode)
	}
	if strings.TrimSpace(e.Body) == "" {
		return errors.New("post: body is empty")
	}
	if e.To != "" && !e.Mode.Addressed() {
		return errors.New("post: only requests can target an agent")
	}
	if e.To != "" && e.To == e.Author {
		return errors.New("post: author cannot target itself")
	}

	const q = `
INSERT INTO entries (room, scope, mode, author, recipient, body, resolves,
                     created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, q,
		e.Room, string(e.Scope), string(e.Mode), e.Author, e.To, e.Body, e.Resolves, formatTime(e.CreatedAt))
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

// Entries treats an empty Rooms list as unfiltered: the API and dashboard list
// every room that way. A caller with a membership-derived list must check it
// for empty first, as roomcmd and searchcmd do.
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
	if len(f.Modes) > 0 {
		where = append(where, "e.mode IN ("+placeholders(len(f.Modes))+")")
		for _, k := range f.Modes {
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
	if f.Limit > 0 {
		// A limit keeps the newest entries, not the oldest: every caller wants
		// recent context, and a briefing that truncated the other way would go
		// stale the moment a room outgrew it. Callers still read oldest first.
		q += " ORDER BY e.id DESC LIMIT ?"
		args = append(args, f.Limit)
		q = "SELECT * FROM (" + q + ") ORDER BY id"
		return s.queryEntries(ctx, q, args...)
	}
	q += " ORDER BY e.id"
	return s.queryEntries(ctx, q, args...)
}

// openPredicate matches requests that still want an answer:
// not itself a resolution, and nothing has closed it.
const openPredicate = `e.mode = 'request' AND e.resolves = 0 AND r.id IS NULL`

// answersToMe matches a resolution of something this session asked. Without it
// an agent is never told that its own request was answered, since a resolution
// is by definition not open.
const answersToMe = `e.resolves != 0 AND EXISTS (
    SELECT 1 FROM entries t WHERE t.id = e.resolves AND t.author = ?)`

func (s *Store) Clear(ctx context.Context, roomKey string) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE room = ?`, roomKey)
	if err != nil {
		return 0, fmt.Errorf("clear room %s: %w", roomKey, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear room %s: %w", roomKey, err)
	}
	return int(n), nil
}

func (s *Store) RemoveEntry(ctx context.Context, id int64, author string) (bool, error) {
	const q = `DELETE FROM entries
WHERE id = ? AND author = ? AND resolves = 0
  AND NOT EXISTS (SELECT 1 FROM entries reply WHERE reply.resolves = entries.id)`
	res, err := s.db.ExecContext(ctx, q, id, author)
	if err != nil {
		return false, fmt.Errorf("remove entry %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("remove entry %d: %w", id, err)
	}
	return n != 0, nil
}

func (s *Store) Unread(ctx context.Context, sessionKey string) ([]*room.Entry, error) {
	const q = `
SELECT ` + entryColumns + entryFrom + `
WHERE e.room IN (SELECT room FROM memberships WHERE session_key = ?)
  AND e.author != ?
  AND (e.recipient = '' OR e.recipient = ?)
  AND e.id > COALESCE((SELECT last_seen FROM cursors WHERE session_key = ?), 0)
  AND ((` + openPredicate + `) OR (` + answersToMe + `))
ORDER BY e.id`
	return s.queryEntries(ctx, q, sessionKey, sessionKey, sessionKey, sessionKey, sessionKey)
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
		mode      string
		createdAt string
	)
	err := sc.Scan(&e.ID, &e.Room, &scope, &mode, &e.Author, &e.To, &e.Body, &e.Resolves,
		&e.ResolvedBy, &createdAt)
	if err != nil {
		return nil, err
	}
	e.Scope = room.Scope(scope)
	e.Mode = room.Mode(mode)
	if e.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	return &e, nil
}

// Search treats an empty Rooms list as unfiltered, as Entries does; see there.
//
// Search matches with LIKE rather than a full-text index. A room holds
// hundreds of rows, not millions, so a scan is instant - and an FTS table would
// need keeping in step with the rows, which is the drift a single source of
// truth exists to avoid.
func (s *Store) Search(ctx context.Context, q room.Query) ([]*room.Entry, error) {
	term := strings.TrimSpace(q.Text)
	if term == "" {
		return nil, errors.New("search: empty query")
	}
	like := "%" + escapeLike(term) + "%"

	roomFilter, roomArgs := "", []any{}
	if len(q.Rooms) > 0 {
		roomFilter = " AND e.room IN (" + placeholders(len(q.Rooms)) + ")"
		for _, r := range q.Rooms {
			roomArgs = append(roomArgs, r)
		}
	}

	entryQ := `SELECT ` + entryColumns + entryFrom + `
 WHERE e.body LIKE ? ESCAPE '!'` + roomFilter + `
 ORDER BY e.id DESC`
	args := append([]any{like}, roomArgs...)
	if q.Limit > 0 {
		entryQ += " LIMIT ?"
		args = append(args, q.Limit)
	}
	return s.queryEntries(ctx, entryQ, args...)
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
