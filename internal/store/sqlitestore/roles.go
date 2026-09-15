package sqlitestore

import (
	"context"
	"errors"
	"fmt"

	"github.com/codyhartsook/multiplayer/internal/role"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// roleActivationSchema is which roles a room has turned on. Kept apart from
// role definitions, which are files: this is the only part of a role's
// existence that lives in the database.
const roleActivationSchema = `
CREATE TABLE IF NOT EXISTS role_activation (
    room         TEXT NOT NULL,
    role         TEXT NOT NULL,
    activated_at TEXT NOT NULL,
    PRIMARY KEY (room, role)
);
`

const roleActivationIndex = `
CREATE INDEX IF NOT EXISTS role_activation_room ON role_activation(room);
`

var _ store.RoleStore = (*Store)(nil)

func (s *Store) ActivateRole(ctx context.Context, a *role.Activation) error {
	if a == nil || a.Room == "" || a.Role == "" {
		return errors.New("activate role: room and role are required")
	}
	const q = `
INSERT INTO role_activation (room, role, activated_at)
VALUES (?, ?, ?)
ON CONFLICT(room, role) DO NOTHING`

	if _, err := s.db.ExecContext(ctx, q, a.Room, a.Role, formatTime(a.ActivatedAt)); err != nil {
		return fmt.Errorf("activate role %s in %s: %w", a.Role, a.Room, err)
	}
	return nil
}

func (s *Store) DeactivateRole(ctx context.Context, roomKey, roleName string) error {
	const q = `DELETE FROM role_activation WHERE room = ? AND role = ?`
	if _, err := s.db.ExecContext(ctx, q, roomKey, roleName); err != nil {
		return fmt.Errorf("deactivate role %s in %s: %w", roleName, roomKey, err)
	}
	return nil
}

func (s *Store) ActiveRoles(ctx context.Context, roomKey string) ([]*role.Activation, error) {
	const q = `SELECT room, role, activated_at FROM role_activation WHERE room = ? ORDER BY role`
	rows, err := s.db.QueryContext(ctx, q, roomKey)
	if err != nil {
		return nil, fmt.Errorf("list active roles: %w", err)
	}
	defer rows.Close()

	out := []*role.Activation{}
	for rows.Next() {
		var (
			a           role.Activation
			activatedAt string
		)
		if err := rows.Scan(&a.Room, &a.Role, &activatedAt); err != nil {
			return nil, fmt.Errorf("list active roles: %w", err)
		}
		if a.ActivatedAt, err = parseTime(activatedAt); err != nil {
			return nil, fmt.Errorf("parse activated_at: %w", err)
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
