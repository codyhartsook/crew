// Package store defines the persistence boundary for session records.
//
// Store is the only interface the hook, API and CLI depend on. The conformance
// suite in internal/store/storetest holds every implementation to the same
// semantics.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/usage"
)

// ErrNotFound is returned when a session id is not present in the store.
var ErrNotFound = errors.New("session not found")

// Filter narrows a List call. Zero-valued fields do not constrain the result.
type Filter struct {
	Harness  session.Harness
	Status   session.Status
	RepoName string
	// RepoRoot matches the working tree the session sits in, exactly.
	RepoRoot string
	// PooledOnly restricts results to sessions inside a pooled worktree.
	PooledOnly bool
	// Limit caps the number of records returned. Zero means no cap.
	Limit int
}

// Store persists agent session records.
//
// Implementations must be safe for concurrent use: several agent harnesses can
// fire session hooks against the same store at the same moment.
type Store interface {
	// Upsert records a session. A repeat write for the same key replaces the
	// mutable fields and preserves StartedAt, so a resumed session keeps its
	// original start time.
	Upsert(ctx context.Context, s *session.Session) error

	// End marks a session ended. It returns ErrNotFound if the key is unknown,
	// and is a no-op on a session that already ended.
	End(ctx context.Context, key string, at time.Time, reason string) error

	// Touch records that a session is still active at time at. It returns
	// ErrNotFound for an unknown key and does nothing to a session that has
	// already ended.
	Touch(ctx context.Context, key string, at time.Time) error

	// SetUsage records what a session has spent. Reported by a turn, so it
	// lands on an active session only; an ended one is left as it was.
	SetUsage(ctx context.Context, key string, u *usage.Snapshot) error

	// Get returns one session by key, or ErrNotFound.
	Get(ctx context.Context, key string) (*session.Session, error)

	// List returns sessions matching f, most recently seen first.
	List(ctx context.Context, f Filter) ([]*session.Session, error)

	// Delete removes a session. Deleting an unknown key returns ErrNotFound.
	Delete(ctx context.Context, key string) error

	// Close releases any resources held by the store.
	Close() error
}
