package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/codyhartsook/multiplayer/internal/rolemem"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// roleMemorySchema is its own table, not a visibility flag on entries.
const roleMemorySchema = `
CREATE TABLE IF NOT EXISTS role_memory (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    room       TEXT NOT NULL,
    role       TEXT NOT NULL,
    author     TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL
);
`

const roleMemoryIndex = `
CREATE INDEX IF NOT EXISTS role_memory_room_role ON role_memory(room, role);
`

var _ store.MemoryStore = (*Store)(nil)

func (s *Store) WriteMemory(ctx context.Context, e *rolemem.Entry) error {
	if e == nil {
		return errors.New("write memory: nil entry")
	}
	if e.Room == "" || e.Role == "" {
		return errors.New("write memory: room and role are required")
	}
	if strings.TrimSpace(e.Body) == "" {
		return errors.New("write memory: body is empty")
	}

	const q = `
INSERT INTO role_memory (room, role, author, body, created_at)
VALUES (?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, q, e.Room, e.Role, e.Author, e.Body, formatTime(e.CreatedAt))
	if err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	e.ID = id
	return nil
}

func (s *Store) ReadMemory(ctx context.Context, f rolemem.Filter) ([]*rolemem.Entry, error) {
	var (
		where []string
		args  []any
	)
	if f.Room != "" {
		where = append(where, "room = ?")
		args = append(args, f.Room)
	}
	if f.Role != "" {
		where = append(where, "role = ?")
		args = append(args, f.Role)
	}

	q := `SELECT id, room, role, author, body, created_at FROM role_memory`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if f.Limit > 0 {
		// Keep the newest entries; a bounded pull wants what's most recent.
		q += " ORDER BY id DESC LIMIT ?"
		args = append(args, f.Limit)
		q = "SELECT * FROM (" + q + ") ORDER BY id"
	} else {
		q += " ORDER BY id"
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}
	defer rows.Close()

	out := []*rolemem.Entry{}
	for rows.Next() {
		var (
			e         rolemem.Entry
			createdAt string
		)
		if err := rows.Scan(&e.ID, &e.Room, &e.Role, &e.Author, &e.Body, &createdAt); err != nil {
			return nil, fmt.Errorf("read memory: %w", err)
		}
		if e.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
