// Package roomdoc builds a room's generated transcript from the store, so the
// dashboard and the CLI produce the same file rather than each their own.
package roomdoc

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/codyhartsook/multiplayer/internal/documents"
	"github.com/codyhartsook/multiplayer/internal/room"
	"github.com/codyhartsook/multiplayer/internal/session"
	"github.com/codyhartsook/multiplayer/internal/store"
)

// Sessions names the participants and the authors of what they posted.
type Sessions interface {
	Get(ctx context.Context, sessionKey string) (*session.Session, error)
	List(ctx context.Context, f store.Filter) ([]*session.Session, error)
}

// Rooms supplies the room's own contents.
type Rooms interface {
	Entries(ctx context.Context, f room.Filter) ([]*room.Entry, error)
	Members(ctx context.Context, roomKey string) ([]*room.Membership, error)
}

// Write regenerates r's transcript beside its documents and returns its path.
// documentDir is the room's document directory.
func Write(ctx context.Context, sessions Sessions, rooms Rooms, documentDir string, r room.Room) (string, error) {
	entries, err := rooms.Entries(ctx, room.Filter{Rooms: []string{r.Key}})
	if err != nil {
		return "", err
	}
	docs, err := documents.List(documentDir)
	if err != nil {
		return "", err
	}
	who, err := participants(ctx, sessions, rooms, r.Key)
	if err != nil {
		return "", err
	}
	named, err := authors(ctx, sessions)
	if err != nil {
		return "", err
	}
	body := room.Snapshot(r, entries, who, documents.Names(docs), named)
	return documents.WriteSnapshot(filepath.Dir(documentDir), body)
}

// participants names the sessions listening to the room. A membership whose
// session is already gone is skipped rather than failing the transcript.
func participants(ctx context.Context, sessions Sessions, rooms Rooms, roomKey string) ([]string, error) {
	memberships, err := rooms.Members(ctx, roomKey)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, membership := range memberships {
		sess, err := sessions.Get(ctx, membership.SessionKey)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		names = append(names, room.Participant(sess))
	}
	return names, nil
}

// authors maps session keys to the aliases entries are rendered with. Ended
// sessions count: the timeline outlives whoever wrote it.
func authors(ctx context.Context, sessions Sessions) (room.Authors, error) {
	list, err := sessions.List(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	out := room.Authors{}
	for _, s := range list {
		if s.Alias != "" {
			out[s.Key()] = s.Alias
		}
	}
	return out, nil
}
