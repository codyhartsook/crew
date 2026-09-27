package sqlitestore

import (
	"context"
	"errors"
	"fmt"

	"github.com/codyhartsook/multiplayer/internal/store"
)

// roleSchema sits beside sessions because SessionStart rewrites the sessions row.
const roleSchema = `
CREATE TABLE IF NOT EXISTS role_assignments (
    session_key TEXT PRIMARY KEY,
    room        TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL,
    assigned_at TEXT NOT NULL
);
`

var _ store.RoleStore = (*Store)(nil)

func (s *Store) Assign(ctx context.Context, r *store.Role) error {
	if r == nil || r.SessionKey == "" || r.Room == "" {
		return errors.New("assign: session key and room are required")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	const q = `
INSERT INTO role_assignments (session_key, room, name, description, assigned_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(session_key) DO UPDATE SET
    room = excluded.room,
    name = excluded.name,
    description = excluded.description,
    assigned_at = excluded.assigned_at`
	if _, err := s.db.ExecContext(ctx, q, r.SessionKey, r.Room, r.Name, r.Description, formatTime(r.AssignedAt)); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	return nil
}

func (s *Store) Drop(ctx context.Context, sessionKey string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM role_assignments WHERE session_key = ?`, sessionKey)
	if err != nil {
		return false, fmt.Errorf("drop role: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) Roles(ctx context.Context, f store.RoleFilter) ([]*store.Role, error) {
	q := `SELECT r.session_key, r.room, r.name, r.description, r.assigned_at FROM role_assignments r`
	var args []any
	if f.ActiveOnly {
		q += ` JOIN sessions s ON s.key = r.session_key AND s.status = 'active'`
	}
	if len(f.Rooms) > 0 {
		q += ` WHERE r.room IN (` + placeholders(len(f.Rooms)) + `)`
		for _, key := range f.Rooms {
			args = append(args, key)
		}
	}
	q += ` ORDER BY r.assigned_at, r.session_key`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var out []*store.Role
	for rows.Next() {
		var (
			r  store.Role
			at string
		)
		if err := rows.Scan(&r.SessionKey, &r.Room, &r.Name, &r.Description, &at); err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		if r.AssignedAt, err = parseTime(at); err != nil {
			return nil, fmt.Errorf("parse assigned_at: %w", err)
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
