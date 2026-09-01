package store

import (
	"context"

	"github.com/codyhartsook/multiplayer/internal/room"
)

// RoomStore persists room entries and the sessions listening to them. Separate
// from Store because a backend may reasonably implement one without the other.
type RoomStore interface {
	// Join adds a session to a room. Joining twice is a no-op that keeps the
	// original join time.
	Join(ctx context.Context, m *room.Membership) error

	// Leave removes a session from a room. Leaving a room the session is not in
	// is not an error.
	Leave(ctx context.Context, sessionKey, roomKey string) error

	// Rooms lists the rooms a session has joined.
	Rooms(ctx context.Context, sessionKey string) ([]*room.Membership, error)

	// Members lists the sessions listening to a room.
	Members(ctx context.Context, roomKey string) ([]*room.Membership, error)

	// Post appends an entry and assigns its ID. Entry ids are monotonic across
	// every room, which is what lets a single per-session cursor track what has
	// been delivered.
	Post(ctx context.Context, e *room.Entry) error

	// Entries lists entries matching f, oldest first.
	Entries(ctx context.Context, f room.Filter) ([]*room.Entry, error)

	// SetState writes a keyed value, replacing any previous one and bumping its
	// revision.
	SetState(ctx context.Context, st *room.State) error

	// GetState returns one value, or ErrNotFound.
	GetState(ctx context.Context, roomKey, key string) (*room.State, error)

	// States lists values matching f, by key.
	States(ctx context.Context, f room.StateFilter) ([]*room.State, error)

	// DeleteState removes a value. Deleting an absent key returns ErrNotFound.
	DeleteState(ctx context.Context, roomKey, key string) error

	// Promote moves an entry, and anything resolving it, to another room.
	// Returns how many rows moved.
	Promote(ctx context.Context, id int64, toRoom string, scope room.Scope) (int, error)

	// PromoteState moves a keyed value. It returns an error naming the clash if
	// the key is already taken in the destination.
	PromoteState(ctx context.Context, fromRoom, key, toRoom string, scope room.Scope) error

	// Search finds state and entries whose text contains the query term.
	Search(ctx context.Context, q room.Query) (*room.Results, error)

	// Clear removes every entry in a room and reports how many went.
	Clear(ctx context.Context, roomKey string) (int, error)

	// RemoveEntry removes an author's unthreaded entry. It reports false when
	// the entry does not exist, belongs to someone else, or has a reply.
	RemoveEntry(ctx context.Context, id int64, author string) (bool, error)

	// Unread returns the open, addressed entries in the rooms this session has
	// joined that it has not yet been shown and did not write itself.
	Unread(ctx context.Context, sessionKey string) ([]*room.Entry, error)

	// Ack records that the session has been shown everything up to and
	// including throughID. Acking an older id does not move the cursor back.
	Ack(ctx context.Context, sessionKey string, throughID int64) error
}
