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

	// Post appends an entry and assigns its ID. IDs are monotonic across every
	// room, so one per-session cursor tracks what has been delivered.
	Post(ctx context.Context, e *room.Entry) error

	// Entries lists entries matching f, oldest first.
	Entries(ctx context.Context, f room.Filter) ([]*room.Entry, error)

	// Search finds entries whose body contains the query term.
	Search(ctx context.Context, q room.Query) ([]*room.Entry, error)

	// Clear removes every entry in a room and reports how many went.
	Clear(ctx context.Context, roomKey string) (int, error)

	// RemoveEntry removes an author's unthreaded entry. It reports false when
	// the entry does not exist, belongs to someone else, or has a reply.
	RemoveEntry(ctx context.Context, id int64, author string) (bool, error)

	// DeleteThread removes an entry and every reply that resolves it, whoever
	// wrote them. It reports false when the entry does not exist.
	DeleteThread(ctx context.Context, id int64) (bool, error)

	// Unread returns the open, addressed entries in the rooms this session has
	// joined that it has not yet been shown and did not write itself.
	Unread(ctx context.Context, sessionKey string) ([]*room.Entry, error)

	// Ack records that the session has been shown everything up to and
	// including throughID. Acking an older id does not move the cursor back.
	Ack(ctx context.Context, sessionKey string, throughID int64) error
}
