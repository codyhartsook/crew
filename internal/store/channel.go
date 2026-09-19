package store

import (
	"context"
	"errors"

	"github.com/codyhartsook/multiplayer/internal/channel"
)

// ChannelStore persists directed requests. Separate from RoomStore because the
// room is broadcast-only. Method names carry the noun because one type
// implements every store interface.
type ChannelStore interface {
	// Ask records an open request and returns its id.
	Ask(ctx context.Context, from, to channel.Addr, body string) (int64, error)

	// Answer closes an open request, false if it is unknown or already
	// answered.
	Answer(ctx context.Context, id int64, body string) (bool, error)

	// GetRequest returns one request by id.
	GetRequest(ctx context.Context, id int64) (*channel.Request, error)

	// Inbox lists the open requests addressed to self, oldest first. An empty
	// address matches nothing, never everything.
	Inbox(ctx context.Context, self channel.Addr) ([]*channel.Request, error)

	// CloseRequests answers every open request from an address with note, so a
	// dead role leaves nothing open. An empty address closes nothing.
	CloseRequests(ctx context.Context, from channel.Addr, note string) (int, error)
}

// OrphanedNote closes a question whose asker will never read the answer.
const OrphanedNote = "(unanswered: the asking role's delegation ended)"

// CloseAsks closes what a finished role left open, so its requester is not
// nagged about a question nobody is waiting on. Both addresses: a role whose
// SessionStart never recorded a child asked as the delegation itself.
func CloseAsks(ctx context.Context, ds DelegationStore, cs ChannelStore, id string) error {
	if cs == nil {
		return nil
	}
	froms := []channel.Addr{channel.Asker("", id)}
	if d, err := ds.GetDelegation(ctx, id); err == nil && d.Child != "" {
		froms = append(froms, channel.Addr(d.Child))
	}
	var errs error
	for _, from := range froms {
		if _, err := cs.CloseRequests(ctx, from, OrphanedNote); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}
