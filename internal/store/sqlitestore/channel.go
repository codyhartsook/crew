package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/channel"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// channelSchema holds directed requests; answered_at IS NULL is open, so there
// is no status column to keep in sync.
const channelSchema = `
CREATE TABLE IF NOT EXISTS channel_requests (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    from_addr   TEXT NOT NULL,
    to_addr     TEXT NOT NULL,
    body        TEXT NOT NULL,
    answer      TEXT NOT NULL DEFAULT '',
    answered_at TEXT,
    created_at  TEXT NOT NULL
);
`

const channelIndex = `
CREATE INDEX IF NOT EXISTS channel_requests_to ON channel_requests(to_addr, answered_at);
`

const channelColumns = `id, from_addr, to_addr, body, answer, answered_at, created_at`

var _ store.ChannelStore = (*Store)(nil)

func (s *Store) Ask(ctx context.Context, from, to channel.Addr, body string) (int64, error) {
	if from == "" || to == "" {
		return 0, errors.New("ask: both addresses are required")
	}
	if from == to {
		return 0, errors.New("ask: cannot ask yourself")
	}
	if strings.TrimSpace(body) == "" {
		return 0, errors.New("ask: body is empty")
	}
	const q = `INSERT INTO channel_requests (from_addr, to_addr, body, created_at) VALUES (?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, q, string(from), string(to), body, formatTime(time.Now()))
	if err != nil {
		return 0, fmt.Errorf("ask %s: %w", to, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("ask %s: %w", to, err)
	}
	return id, nil
}

func (s *Store) Answer(ctx context.Context, id int64, body string) (bool, error) {
	if strings.TrimSpace(body) == "" {
		return false, errors.New("answer: body is empty")
	}
	const q = `UPDATE channel_requests SET answer = ?, answered_at = ? WHERE id = ? AND answered_at IS NULL`
	res, err := s.db.ExecContext(ctx, q, body, formatTime(time.Now()), id)
	if err != nil {
		return false, fmt.Errorf("answer %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("answer %d: %w", id, err)
	}
	return n > 0, nil
}

func (s *Store) GetRequest(ctx context.Context, id int64) (*channel.Request, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM channel_requests WHERE id = ?`, id)
	r, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("request %d: %w", id, store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get request %d: %w", id, err)
	}
	return r, nil
}

// Inbox is addressed-only: an empty address matches nothing, since falling
// back to unfiltered is how the room read leak worked.
func (s *Store) Inbox(ctx context.Context, self channel.Addr) ([]*channel.Request, error) {
	if self == "" {
		return nil, nil
	}
	const q = `SELECT ` + channelColumns + ` FROM channel_requests
WHERE to_addr = ? AND answered_at IS NULL ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, string(self))
	if err != nil {
		return nil, fmt.Errorf("inbox %s: %w", self, err)
	}
	defer rows.Close()

	out := []*channel.Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("inbox %s: %w", self, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CloseRequests(ctx context.Context, from channel.Addr, note string) (int, error) {
	if from == "" {
		return 0, nil
	}
	const q = `UPDATE channel_requests SET answer = ?, answered_at = ?
WHERE from_addr = ? AND answered_at IS NULL`
	res, err := s.db.ExecContext(ctx, q, note, formatTime(time.Now()), string(from))
	if err != nil {
		return 0, fmt.Errorf("close requests from %s: %w", from, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("close requests from %s: %w", from, err)
	}
	return int(n), nil
}

func scanRequest(sc scanner) (*channel.Request, error) {
	var (
		r          channel.Request
		from, to   string
		answeredAt sql.NullString
		createdAt  string
	)
	if err := sc.Scan(&r.ID, &from, &to, &r.Body, &r.Answer, &answeredAt, &createdAt); err != nil {
		return nil, err
	}
	r.From, r.To = channel.Addr(from), channel.Addr(to)
	if answeredAt.Valid {
		t, err := parseTime(answeredAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse answered_at: %w", err)
		}
		r.AnsweredAt = &t
	}
	var err error
	if r.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	return &r, nil
}
