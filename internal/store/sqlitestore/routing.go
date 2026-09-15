package sqlitestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/codyhartsook/multiplayer/internal/routing"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// routingLogSchema records every roster shown; no "taken" column yet.
const routingLogSchema = `
CREATE TABLE IF NOT EXISTS routing_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    room       TEXT NOT NULL,
    session    TEXT NOT NULL,
    roles      TEXT NOT NULL,
    created_at TEXT NOT NULL
);
`

const routingLogIndex = `
CREATE INDEX IF NOT EXISTS routing_log_room ON routing_log(room);
`

var _ store.RoutingStore = (*Store)(nil)

func (s *Store) LogRouting(ctx context.Context, d *routing.Decision) error {
	if d == nil || d.Room == "" || len(d.Roles) == 0 {
		return errors.New("log routing: room and at least one role are required")
	}
	roles, err := json.Marshal(d.Roles)
	if err != nil {
		return fmt.Errorf("encode roles: %w", err)
	}

	const q = `
INSERT INTO routing_log (room, session, roles, created_at)
VALUES (?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, q, d.Room, d.Session, string(roles), formatTime(d.CreatedAt))
	if err != nil {
		return fmt.Errorf("log routing: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("log routing: %w", err)
	}
	d.ID = id
	return nil
}

func (s *Store) RoutingLog(ctx context.Context, roomKey string) ([]*routing.Decision, error) {
	const q = `SELECT id, room, session, roles, created_at FROM routing_log WHERE room = ? ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, roomKey)
	if err != nil {
		return nil, fmt.Errorf("list routing log: %w", err)
	}
	defer rows.Close()

	out := []*routing.Decision{}
	for rows.Next() {
		var (
			d         routing.Decision
			rolesJSON string
			createdAt string
		)
		if err := rows.Scan(&d.ID, &d.Room, &d.Session, &rolesJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("list routing log: %w", err)
		}
		if err := json.Unmarshal([]byte(rolesJSON), &d.Roles); err != nil {
			return nil, fmt.Errorf("decode roles: %w", err)
		}
		if d.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}
