package detect

import (
	"context"
	"errors"

	"github.com/codyhartsook/multiplayer/internal/session"
)

// Anchor recognizes the workspace that owns a directory. Recognizing another
// kind of workspace is a new implementation, not a change to the model.
type Anchor interface {
	// Name is the kind of workspace this anchor speaks for.
	Name() string
	// Lookup returns the place owning dir, or nil for a directory this anchor
	// does not recognize. Not recognizing one is ordinary, not a failure.
	Lookup(ctx context.Context, dir string) (*session.Place, error)
}

// ErrAnchorUnavailable reports that an anchor cannot run here, so the chain moves
// on. Any other error stops it, so a later anchor cannot claim the directory.
var ErrAnchorUnavailable = errors.New("anchor unavailable")

// anchors are tried in order, first match winning. Git leads: honouring a
// marker inside a checkout would split that repository's room.
var anchors = []Anchor{gitAnchor{}, folderAnchor{}}
