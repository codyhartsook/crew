package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codyhartsook/multiplayer/internal/delegation"
	"github.com/codyhartsook/multiplayer/internal/store"
)

const delegationSchema = `
CREATE TABLE IF NOT EXISTS delegations (
    id         TEXT PRIMARY KEY,
    room       TEXT NOT NULL,
    dir        TEXT NOT NULL DEFAULT '',
    role       TEXT NOT NULL,
    harness    TEXT NOT NULL,
    requester  TEXT NOT NULL,
    prompt     TEXT NOT NULL,
    status     TEXT NOT NULL,
    result     TEXT NOT NULL DEFAULT '',
    error      TEXT NOT NULL DEFAULT '',
    notified   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
`

const delegationIndexStatus = `
CREATE INDEX IF NOT EXISTS delegations_status ON delegations(status);
`

const delegationIndexRequester = `
CREATE INDEX IF NOT EXISTS delegations_requester ON delegations(requester, notified);
`

const delegationColumns = `id, room, dir, role, harness, requester, prompt, status, result, error, notified, created_at, updated_at`

var _ store.DelegationStore = (*Store)(nil)

func (s *Store) CreateDelegation(ctx context.Context, d *delegation.Delegation) error {
	if d == nil || d.ID == "" || d.Room == "" || d.Role == "" {
		return errors.New("create delegation: id, room, and role are required")
	}
	const q = `
INSERT INTO delegations (` + delegationColumns + `)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, q,
		d.ID, d.Room, d.Dir, d.Role, d.Harness, d.Requester, d.Prompt, string(d.Status),
		d.Result, d.Error, d.Notified, formatTime(d.CreatedAt), formatTime(d.UpdatedAt))
	if err != nil {
		return fmt.Errorf("create delegation %s: %w", d.ID, err)
	}
	return nil
}

func (s *Store) GetDelegation(ctx context.Context, id string) (*delegation.Delegation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+delegationColumns+` FROM delegations WHERE id = ?`, id)
	d, err := scanDelegation(row)
	if err != nil {
		return nil, fmt.Errorf("get delegation %s: %w", id, err)
	}
	return d, nil
}

func (s *Store) ListDelegations(ctx context.Context, f delegation.Filter) ([]*delegation.Delegation, error) {
	var (
		where []string
		args  []any
	)
	if f.Requester != "" {
		where = append(where, "requester = ?")
		args = append(args, f.Requester)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(f.Status))
	}
	if f.Unnotified {
		where = append(where, "notified = 0")
	}

	q := `SELECT ` + delegationColumns + ` FROM delegations`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list delegations: %w", err)
	}
	defer rows.Close()

	out := []*delegation.Delegation{}
	for rows.Next() {
		d, err := scanDelegation(rows)
		if err != nil {
			return nil, fmt.Errorf("list delegations: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) StartDelegation(ctx context.Context, id string) (bool, error) {
	const q = `UPDATE delegations SET status = ?, updated_at = ? WHERE id = ? AND status = ?`
	res, err := s.db.ExecContext(ctx, q, string(delegation.StatusRunning), formatTime(time.Now()), id, string(delegation.StatusPending))
	if err != nil {
		return false, fmt.Errorf("start delegation %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("start delegation %s: %w", id, err)
	}
	return n > 0, nil
}

func (s *Store) CompleteDelegation(ctx context.Context, id, result string) error {
	const q = `UPDATE delegations SET status = ?, result = ?, updated_at = ? WHERE id = ? AND status = ?`
	_, err := s.db.ExecContext(ctx, q, string(delegation.StatusDone), result, formatTime(time.Now()), id, string(delegation.StatusRunning))
	if err != nil {
		return fmt.Errorf("complete delegation %s: %w", id, err)
	}
	return nil
}

func (s *Store) FailDelegation(ctx context.Context, id, errMsg string) error {
	const q = `UPDATE delegations SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`
	_, err := s.db.ExecContext(ctx, q, string(delegation.StatusFailed), errMsg, formatTime(time.Now()), id, string(delegation.StatusRunning))
	if err != nil {
		return fmt.Errorf("fail delegation %s: %w", id, err)
	}
	return nil
}

func (s *Store) MarkNotified(ctx context.Context, id string) error {
	const q = `UPDATE delegations SET notified = 1 WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("mark delegation %s notified: %w", id, err)
	}
	return nil
}

func scanDelegation(sc scanner) (*delegation.Delegation, error) {
	var (
		d         delegation.Delegation
		status    string
		notified  bool
		createdAt string
		updatedAt string
	)
	err := sc.Scan(&d.ID, &d.Room, &d.Dir, &d.Role, &d.Harness, &d.Requester, &d.Prompt,
		&status, &d.Result, &d.Error, &notified, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	d.Status = delegation.Status(status)
	d.Notified = notified
	if d.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if d.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &d, nil
}
